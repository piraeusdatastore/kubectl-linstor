# kubectl-linstor

A plugin to execute LINSTOR commands via the kubectl command line. You can think of it as an alias for 
`kubectl exec <linstor-controller-pod> -- linstor ...`. 

```
$ kubectl linstor node list
╭────────────────────────────────────────────────────────────────────────────────────────────╮
┊ Node                                   ┊ NodeType   ┊ Addresses                   ┊ State  ┊
╞════════════════════════════════════════════════════════════════════════════════════════════╡
┊ kube-node-01.test                      ┊ SATELLITE  ┊ 10.43.224.26:3366 (PLAIN)   ┊ Online ┊
┊ kube-node-02.test                      ┊ SATELLITE  ┊ 10.43.224.27:3366 (PLAIN)   ┊ Online ┊
┊ kube-node-03.test                      ┊ SATELLITE  ┊ 10.43.224.28:3366 (PLAIN)   ┊ Online ┊
┊ linstor-cs-controller-85b4f757f5-kxdvn ┊ CONTROLLER ┊ 172.24.116.114:3366 (PLAIN) ┊ Online ┊
╰────────────────────────────────────────────────────────────────────────────────────────────╯
```

## Requirements & Installation

To use `kubectl linstor` you need to be using the Piraeus Operator. You need exactly one `LinstorController` resource
in your cluster. You can verify this by running:

```
$ kubectl get linstorcontrollers.linstor.linbit.com --all-namespaces
NAMESPACE   NAME            AGE
piraeus     piraeus-op-cs   5d21h
```

In addition, the plugin makes use of the `kubectl` command itself. Pointing `kubectl` to another cluster or using a
different user will also affect `kubectl linstor`. 

To install the plugin, head to [the release page](https://github.com/piraeusdatastore/kubectl-linstor/releases) and grab
the latest release. After downloading unpack it somewhere in your `PATH`.

## Usage

`kubectl linstor` runs the normal LINSTOR client configured for your cluster. Don't worry about installing the client,
the plugin will execute the command in the controller pod, where this client is already installed.

To learn how to use the LINSTOR client, head over to [the LINSTOR user guide](https://www.linbit.com/drbd-user-guide/linstor-guide-1_0-en).

`kubectl linstor` also comes with some additional Kubernetes integrations:

#### Mapping PersistentVolumeClaims (PVCs) to LINSTOR resources

Every argument that matches `pvc:[<namespace>/]<pvcname>` will be expanded by `kubectl linstor` to use the LINSTOR resource
backing the PersistentVolume that is bound to the PVC. It will also print how the argument was mapped:
```
$ kubectl linstor resource list -r pvc:piraeus/demo-pvc-1 --all
pvc:piraeus/demo-pvc-1 -> pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526
╭─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
┊ ResourceName                             ┊ Node              ┊ Port ┊ Usage  ┊ Conns ┊    State   ┊ CreatedOn           ┊
╞═════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════╡
┊ pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526 ┊ kube-node-01.test ┊ 7000 ┊ Unused ┊ Ok    ┊   UpToDate ┊ 2021-02-05 09:16:09 ┊
┊ pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526 ┊ kube-node-02.test ┊ 7000 ┊ Unused ┊ Ok    ┊ TieBreaker ┊ 2021-02-05 09:16:08 ┊
┊ pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526 ┊ kube-node-03.test ┊ 7000 ┊ InUse  ┊ Ok    ┊   UpToDate ┊ 2021-02-05 09:16:09 ┊
╰─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

#### Mapping Pods to LINSTOR resources

Every argument that matches `pod:[<namespace>/]<pvcname>` will also be expanded by `kubectl linstor`. The Pod is first
mapped to the PersistentVolumeClaims it's referencing. Those are again mapped to the LINSTOR resources:
```
$ kubectl linstor resource list -r pod:piraeus/demo-pod-1 --all
pod:piraeus/demo-pod-1 -> pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526 pvc-29bda4a8-90fc-4029-b5e0-c1ccfcc46cf9
╭─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
┊ ResourceName                             ┊ Node              ┊ Port ┊ Usage  ┊ Conns ┊    State   ┊ CreatedOn           ┊
╞═════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════╡
┊ pvc-29bda4a8-90fc-4029-b5e0-c1ccfcc46cf9 ┊ kube-node-01.test ┊ 7001 ┊ Unused ┊ Ok    ┊   UpToDate ┊ 2021-02-09 14:16:18 ┊
┊ pvc-29bda4a8-90fc-4029-b5e0-c1ccfcc46cf9 ┊ kube-node-02.test ┊ 7001 ┊ Unused ┊ Ok    ┊ TieBreaker ┊ 2021-02-09 14:16:18 ┊
┊ pvc-29bda4a8-90fc-4029-b5e0-c1ccfcc46cf9 ┊ kube-node-03.test ┊ 7001 ┊ InUse  ┊ Ok    ┊   UpToDate ┊ 2021-02-09 14:16:19 ┊
┊ pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526 ┊ kube-node-01.test ┊ 7000 ┊ Unused ┊ Ok    ┊   UpToDate ┊ 2021-02-05 09:16:09 ┊
┊ pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526 ┊ kube-node-02.test ┊ 7000 ┊ Unused ┊ Ok    ┊ TieBreaker ┊ 2021-02-05 09:16:08 ┊
┊ pvc-2f982fb4-bc05-4ee5-b15b-688b696c8526 ┊ kube-node-03.test ┊ 7000 ┊ InUse  ┊ Ok    ┊   UpToDate ┊ 2021-02-05 09:16:09 ┊
╰─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

# Contributing

See [piraeus/CONTRIBUTING](https://github.com/piraeusdatastore/piraeus/blob/master/CONTRIBUTING.md)

# License

License under the Apache License, Version 2.0. See [LICENSE](./LICENSE)
