[![Build Status](https://shuanglu1993.visualstudio.com/k8sTcpdump/_apis/build/status/Shuanglu.k8sTcpdump?branchName=main)](https://shuanglu1993.visualstudio.com/k8sTcpdump/_build/latest?definitionId=8&branchName=main)

### k8sTcpdump

### Intro
The tool is to use the 'tcpdump' to capture the network trace of the pod

### Prerequistes
1. The access to create privileged pod
2. The access to view the pod you would like to capture


### Usage:

![](https://github.com/Shuanglu/k8sTcpdump/blob/main/doc/k8sTcpdump.gif)

1. Input the "name" of the pod and "namespace" of the pod to the "xxx.json". Example is "example/test.json"
2. Run "./k8sTcpdump -p xxx.json" and it will bring up pods on the corresponding nodes to capture the network traces of the target pods. The '.cap' file will be downoaded to the current folder.
