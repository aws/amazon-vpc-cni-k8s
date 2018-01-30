# eni-ip-controller

eni-ip-controller manages the number of available vpc ip addresses on a node as [kubernete's extended resources](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#extended-resources). And it uses **eks.amazon.com/ip** as a node level resource. 

eni-ip-controller watches node resource in the cluster. Whenever there is a new node joined the cluster, it updates **eks.amazon.com/ip** resource for that node.

## Pod Specification
For a pod which is not using **hostNetwork** mode needs to specify following in one of its container:

```
vpiVersion: v1
  kind: Pod
  metadata:
    name: my-pod
spec:
  containers:
  - name: my-container
    image: myimage
    resource:
      requests:
        eks.amazon.com/ip: 1
      limits:
        eks.amazon.com/ip: 1
``` 

## Install

### Prerequisites

* Kubernetes v1.8+, since kubernetes version 1.8 introduces Extended Resoures

### Install eni-ip-controller

```
kubectl apply -f eni-ip-controller.yaml
```

## Build

```
go build -o eni-ip-controller eni_ip_controller.go
```