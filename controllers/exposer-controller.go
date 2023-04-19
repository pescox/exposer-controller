package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	//v1 "k8s.io/client-go/applyconfigurations/core/v1"
	"time"

	appinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	applisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	baseDomain     = "example.com"
	applyNamespace = "exposer-test"
)

type exposerController struct {
	clientset      kubernetes.Interface
	depCacheSynced cache.InformerSynced
	depLister      applisters.DeploymentLister
	queue          workqueue.RateLimitingInterface
}

func NewExposerController(clientset kubernetes.Interface, depInformer appinformers.DeploymentInformer) *exposerController {
	c := &exposerController{
		clientset:      clientset,
		depCacheSynced: depInformer.Informer().HasSynced,
		depLister:      depInformer.Lister(),
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "exposer"),
	}

	depInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleAdd,
		DeleteFunc: c.handleDel,
	})
	return c
}

func (c *exposerController) Run(stopCh <-chan struct{}) {
	fmt.Println("starting exposer controller.")
	if !cache.WaitForCacheSync(stopCh, c.depCacheSynced) {
		fmt.Println("waiting cache to be synced.")
	}

	go wait.Until(c.worker, time.Second, stopCh)
	<-stopCh
}

func (c *exposerController) processItem() bool {
	item, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	defer c.queue.Forget(item)
	key, err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		fmt.Printf("getting key from cache err: %s\n", err.Error())
		return false
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		fmt.Printf("splitting key to namespace and name err: %s\n", err.Error())
		return false
	}

	// check if the object has been deleted from k8s cluster
	ctx := context.Background()
	_, err = c.clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// delete ing
		if err = c.clientset.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("delete ingress err: %s\n", err.Error())
		} else {
			fmt.Printf("deleted ing [%s.%s]\n", name, ns)
		}
		// delete svc
		if err = c.clientset.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("delete service err: %s\n", err.Error())
			return false
		} else {
			fmt.Printf("deleted svc [%s.%s]\n", name, ns)
			return true
		}
	}

	err = c.syncDeployment(ns, name)
	if err != nil {
		// retry
		fmt.Printf("syncing deployment err: %s", err.Error())
		return false
	}
	return true
}

func (c *exposerController) syncDeployment(namespace, name string) error {
	dep, err := c.depLister.Deployments(namespace).Get(name)
	if err != nil {
		fmt.Printf("sync Deployment err: %s\n", err.Error())
		return err
	}

	if namespace == applyNamespace {
		fmt.Println(namespace)
		// get svc
		_, err := c.clientset.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// create svc
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      dep.Name,
						Namespace: namespace,
					},
					// we have to modify this, to figure out the port
					// our deployment's container is listening on
					Spec: corev1.ServiceSpec{
						Selector: depPodLabels(dep),
						Ports:    makeupServicePorts(depPodPorts(dep)),
					},
				}
				_, err = c.clientset.CoreV1().Services(namespace).Create(context.Background(), svc, metav1.CreateOptions{})
				if err != nil {
					fmt.Printf("creating service err: %s\n", err.Error())
					return err
				}
				fmt.Printf("created svc [%s.%s]\n", svc.GetName(), svc.GetNamespace())
			} else {
				fmt.Printf("creating service err: %s\n", err.Error())
			}
		}

		// get ing
		_, err = c.clientset.NetworkingV1().Ingresses(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// create ing
				ing := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      dep.Name,
						Namespace: namespace,
						Annotations: map[string]string{
							"nginx.ingress.kubernetes.io/rewrite-target": "/",
						},
					},
					Spec: networkingv1.IngressSpec{
						Rules: []networkingv1.IngressRule{
							{
								// you can modify the host generation logic
								Host: fmt.Sprintf("%s-%s.%s", namespace, name, baseDomain),
								IngressRuleValue: networkingv1.IngressRuleValue{
									HTTP: &networkingv1.HTTPIngressRuleValue{
										Paths: makeupIngressPaths(name, depPodPorts(dep)),
									},
								},
							},
						},
					},
				}
				if _, err = c.clientset.NetworkingV1().Ingresses(namespace).Create(context.Background(), ing, metav1.CreateOptions{}); err != nil {
					fmt.Printf("creating ingress err: %s\n", err.Error())
					return err
				}
				fmt.Printf("created ing [%s.%s]\n", ing.GetName(), ing.GetNamespace())
			} else {
				fmt.Printf("creating ingress err: %s\n", err.Error())
			}
		}
	}

	return nil
}

func depPodLabels(dep *appsv1.Deployment) map[string]string {
	return dep.Spec.Template.Labels
}

func depPodPorts(dep *appsv1.Deployment) map[string]int32 {
	res := make(map[string]int32)
	// todo: maybe you modify this to determine if
	// the first container is the main container
	mainContainerPorts := make([]corev1.ContainerPort, 0)
	mainContainerPorts = append(mainContainerPorts, dep.Spec.Template.Spec.Containers[0].Ports...)
	if len(mainContainerPorts) == 0 {
		mainContainerPorts = append(mainContainerPorts, corev1.ContainerPort{
			Name:          "http",
			Protocol:      "tcp",
			ContainerPort: 80,
		})
	}

	for _, port := range mainContainerPorts {
		res[port.Name] = port.ContainerPort
	}
	return res
}

func makeupServicePorts(rawPorts map[string]int32) []corev1.ServicePort {
	res := make([]corev1.ServicePort, 0)
	for name, port := range rawPorts {
		res = append(res, corev1.ServicePort{
			Name:       name,
			Port:       port,
			TargetPort: intstr.FromInt(int(port)),
		})
	}
	return res
}

func makeupIngressPaths(svc string, rawPorts map[string]int32) []networkingv1.HTTPIngressPath {
	res := make([]networkingv1.HTTPIngressPath, 0)
	defaultPathType := networkingv1.PathTypePrefix
	for _, port := range rawPorts {
		res = append(res, networkingv1.HTTPIngressPath{
			Path: "/",
			//Path:     fmt.Sprintf("/%s", svc),
			PathType: &defaultPathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: svc,
					Port: networkingv1.ServiceBackendPort{
						Number: port,
					},
				},
			},
		})
	}
	return res
}

func (c *exposerController) worker() {
	for c.processItem() {
	}
}

func (c *exposerController) handleAdd(obj interface{}) {
	if dep, ok := obj.(*appsv1.Deployment); ok {
		fmt.Printf("deployment [%s.%s] added\n", dep.GetName(), dep.GetNamespace())
		if dep.GetNamespace() == applyNamespace {
			c.queue.Add(obj)
		}
	}
}

func (c *exposerController) handleDel(obj interface{}) {
	if dep, ok := obj.(*appsv1.Deployment); ok {
		fmt.Printf("deployment [%s.%s] deleted\n", dep.GetName(), dep.GetNamespace())
		if dep.GetNamespace() == applyNamespace {
			c.queue.Add(obj)
		}
	}
}
