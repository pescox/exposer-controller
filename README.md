# exposer

## build exposer

```bash
go build -o exposer .
```

## run exposer

```bash
./exposer
```

## test exposer

```bash
kubectl create ns exposer-test
kubectl create deployment nginx -n exposer-test --image=nginx
kubectl get svc ing -n exposer-test
```

## clean up

```bash
kubectl delete deployment nginx -n exposer-test
kubectl delete svc nginx -n exposer-test
kubectl delete ing nginx -n exposer-test
```
