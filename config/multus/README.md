## Setup
The following Multus installation will set up aws-vpc-cni as the default plugin.

The Multus daemonset assumes that 10-aws.conflist (aws-vpc-cni) config file is present in the /etc/cni/net.d/ directory on your worker node to generate the multus config from it. 

Download the latest version of the [yaml](../multus/) and apply it the cluster.
```
kubectl apply -f aws-k8s-multus.yaml
```

Check logs for Multus pod
```
kubectl logs -f pod/kube-multus-ds-677mq -n kube-system
2021-07-27T08:02:00+0000 Generating Multus configuration file using files in /host/etc/cni/net.d...
2021-07-27T08:02:00+0000 Using MASTER_PLUGIN: 10-aws.conflist
2021-07-27T08:02:00+0000 Nested capabilities string: "capabilities": {"portMappings": true},
2021-07-27T08:02:00+0000 Using /host/etc/cni/net.d/10-aws.conflist as a source to generate the Multus configuration
2021-07-27T08:02:00+0000 Config file created @ /host/etc/cni/net.d/00-multus.conf
{ "cniVersion": "0.3.1", "name": "multus-cni-network", "type": "multus", "capabilities": {"portMappings": true}, "kubeconfig": "/etc/cni/net.d/multus.d/multus.kubeconfig", "delegates": [ { "cniVersion": "0.3.1", "name": "aws-cni", "plugins": [ { "name": "aws-cni", "type": "aws-cni", "vethPrefix": "eni", "mtu": "9001", "pluginLogFile": "/var/log/aws-routed-eni/plugin.log", "pluginLogLevel": "DEBUG" }, { "type": "portmap", "capabilities": {"portMappings": true}, "snat": true } ] } ] }
2021-07-27T08:02:00+0000 Entering sleep (success)...
```

### Multus Logging
The log level for multus has been set to error. It will log only the errors.

If you want a more verbose logging you can change it to verbose. You can also use debug for your development but it can quickly fill up disk space

You can find more info [here](https://github.com/k8snetworkplumbingwg/multus-cni/blob/master/docs/configuration.md#logging-level)
