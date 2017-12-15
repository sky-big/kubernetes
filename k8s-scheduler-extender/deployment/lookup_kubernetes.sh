#!/bin/bash

function operate() {
    case $1 in
        node)
            kubectl get node -o wide --server=192.168.152.134:8080
            ;;
        delete)
            kubectl delete deployment tiller-deploy --namespace=kube-system
            kubectl delete svc tiller-deploy --namespace=kube-system
            ;;
        *)
            sudo kubectl get $1 --all-namespaces -o wide --server=192.168.152.134:8080
            ;;
    esac
}

operate $1
