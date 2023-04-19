package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/poneding/exposer-controller/controllers"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	clientset, e := KubeClient()
	if e != nil {
		fmt.Printf("err: %s\n", e.Error())
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Second*10)

	stopCh := make(chan struct{})

	ec := controllers.NewExposerController(clientset, informerFactory.Apps().V1().Deployments())
	informerFactory.Start(stopCh)
	ec.Run(stopCh)
}

// KubeConfig read kubernetes config from kubeconfig file
func KubeClient() (*kubernetes.Clientset, error) {
	kubeconfig := flag.String("kubeconfig", "/home/dp/.kube/config", "location to your kubeconfig file")
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		fmt.Printf("error %s building config from flags\n", err.Error())
		config, err = rest.InClusterConfig()
		if err != nil {
			fmt.Printf("error %s getting inclusterconfig\n", err.Error())
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}
