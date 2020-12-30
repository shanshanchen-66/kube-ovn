package controller

import (
	"fmt"
	"reflect"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ipam"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	AddPodEvent = "add_pod_event"
	DelPodEvent = "del_pod_event"
)

func (c *Controller) runAddVnfGroupWorker() {
	for c.processNextAddOrUpdateVnfWorkItem() {
	}
}

func (c *Controller) runDelVnfGroupWorker() {
	for c.processNextDelVnfWorkItem() {
	}
}

func (c *Controller) enqueueAddVnf(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add vnfGroup %s", key)
	c.addOrUpdateVnfGroupQueue.Add(key)
}

func (c *Controller) enqueueUpdateVnf(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldVnf := old.(*kubeovnv1.VnfGroup)
	newVnf := new.(*kubeovnv1.VnfGroup)

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}

	if !newVnf.DeletionTimestamp.IsZero() && len(newVnf.Status.Ports) == 0 {
		klog.Infof("enqueue update vnfGroup %s", key)
		c.addOrUpdateVnfGroupQueue.Add(key)
		return
	}

	if !reflect.DeepEqual(oldVnf.Spec.Subnet, newVnf.Spec.Subnet) ||
		!reflect.DeepEqual(oldVnf.Spec.Ips, newVnf.Spec.Ips) {
		klog.Infof("enqueue update vnfGroup %s", key)
		c.addOrUpdateVnfGroupQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVnf(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue delete vnf %s", key)
	c.delVnfGroupQueue.Add(key)
}

func (c *Controller) processNextAddOrUpdateVnfWorkItem() bool {
	obj, shutdown := c.addOrUpdateVnfGroupQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOrUpdateVnfGroupQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdateVnfGroupQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOrUpdateVnfGroup(key); err != nil {
			c.addOrUpdateVnfGroupQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOrUpdateVnfGroupQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDelVnfWorkItem() bool {
	obj, shutdown := c.delVnfGroupQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delVnfGroupQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delVnfGroupQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelVnfGroup(key); err != nil {
			c.delVnfGroupQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delVnfGroupQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddOrUpdateVnfGroup(key string) error {
	c.vnfGroupKeyMutex.Lock(key)
	defer c.vnfGroupKeyMutex.Unlock(key)

	vnfGroup, err := c.vnfGroupLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	needUpdate := false
	// can not change subnet of vnf
	if vnfGroup.Status.Subnet != "" && vnfGroup.Spec.Subnet != vnfGroup.Status.Subnet {
		vnfGroup.Spec.Subnet = vnfGroup.Status.Subnet
		needUpdate = true
	}

	checkIps := util.UniqString(vnfGroup.Spec.Ips)
	if len(checkIps) != len(vnfGroup.Spec.Ips) {
		vnfGroup.Spec.Ips = checkIps
		needUpdate = true
	}

	if needUpdate {
		if _, err = c.config.KubeOvnClient.KubeovnV1().VnfGroups().Update(vnfGroup); err != nil {
			return err
		}
		return nil
	}
	return c.reconcilePortPair(vnfGroup)
}

func (c *Controller) reconcilePortPair(vnfGroup *kubeovnv1.VnfGroup) error {
	var vnfPorts []string
	subnet := c.ipam.Subnets[vnfGroup.Spec.Subnet]
	for _, ip := range vnfGroup.Spec.Ips {
		if podKey, ok := subnet.IPToPod[ipam.IP(ip)]; ok {
			namespace, name, err := cache.SplitMetaNamespaceKey(podKey)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("invalid pod key: %s", podKey))
				return nil
			}
			pod, err := c.podsLister.Pods(namespace).Get(name)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					klog.Infof("pod no found")
					continue
				}
			}
			vnfPorts = append(vnfPorts, fmt.Sprintf("%s.%s", pod.Name, pod.Namespace))
		} else {
			klog.Infof("podkey no found")
		}
	}

	klog.Infof("%v", vnfPorts)
	if len(vnfPorts) == len(vnfGroup.Status.Ports) && len(util.DiffStringSlice(vnfPorts, vnfGroup.Status.Ports)) == 0 {
		return nil
	}

	// clean invalid port pair group in sfc
	sfcs, err := c.getSfcWithVnfGroup(vnfGroup.Name)
	if err != nil {
		return err
	}
	for _, sfc := range sfcs {
		if err = c.ovnClient.DelLogicalPortPairGroup(genPortPairGroupName(sfc, vnfGroup.Name)); err != nil {
			return err
		}
	}

	// clean invalid port pair
	for _, port := range vnfGroup.Status.Ports {
		if !util.ContainsString(vnfPorts, port) {
			if err := c.ovnClient.DelLogicalPortPair(genPortPairName(vnfGroup.Name, port)); err != nil {
				klog.Errorf("failed to delete port pair, %v", err)
				return err
			}
		}
	}

	// create new port pair
	for _, port := range vnfPorts {
		if !util.ContainsString(vnfGroup.Status.Ports, port) {
			if err := c.ovnClient.CreatePortPair(genPortPairName(vnfGroup.Name, port), vnfGroup.Spec.Subnet, port, port); err != nil {
				klog.Errorf("failed to create port pair, %v", err)
				return err
			}
		}
	}

	vnfGroup.Status.Ports = vnfPorts
	vnfGroup.Status.Subnet = vnfGroup.Spec.Subnet
	bytes, err := vnfGroup.Status.Bytes()
	if err != nil {
		return err
	}

	_, err = c.config.KubeOvnClient.KubeovnV1().VnfGroups().Patch(vnfGroup.Name, types.MergePatchType, bytes, "status")
	if err != nil {
		return err
	}
	if err := c.reconcileSfcs(vnfGroup); err != nil {
		return err
	}

	return nil
}

func genPortPairName(vnfGroupName, port string) string {
	return fmt.Sprintf("%s-pp-%s", vnfGroupName, port)
}

func (c *Controller) handleDelVnfGroup(key string) error {
	c.vnfGroupKeyMutex.Lock(key)
	defer c.vnfGroupKeyMutex.Unlock(key)

	klog.Infof("start to gc port pair of vnf group '%s'", key)
	pps, err := c.ovnClient.ListLogicalPortPair()
	if err != nil {
		klog.Errorf("failed to list port pair, %v", err)
		return err
	}

	// clean the port_pair_groups which associated with vnfGroup
	sfcs, err := c.getSfcWithVnfGroup(key)
	if err != nil {
		return err
	}
	for _, sfc := range sfcs {
		if err = c.ovnClient.DelLogicalPortPairGroup(genPortPairGroupName(sfc, key)); err != nil {
			return err
		}
	}

	// clean port pair of vnfGroup
	for _, pp := range pps {
		result := strings.Split(pp, "-pp-")
		if len(result) != 2 || result[0] == key {
			if err := c.ovnClient.DelLogicalPortPair(pp); err != nil {
				klog.Errorf("failed to delete port pair %s , %v", pp, err)
				return err
			}
		}
	}

	for _, sfc := range sfcs {
		c.addOrUpdateSfcQueue.Add(sfc)
	}
	return nil
}

func (c *Controller) reconcileVnfGroupPorts(podName, namespace, podEvent string) error {
	var subnet, ip string
	var hasSubnet, hasIp bool
	if podEvent == AddPodEvent {
		pod, err := c.podsLister.Pods(namespace).Get(podName)
		if err != nil {
			klog.Errorf("failed to get pod %s, %v", podName, err)
			return err
		} else {
			subnet, hasSubnet = pod.Annotations[util.LogicalSwitchAnnotation]
			ip, hasIp = pod.Annotations[util.IpAddressAnnotation]
			if !hasSubnet || !hasIp {
				return nil
			}
		}
	}

	vnfGroups, err := c.vnfGroupLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get vnfGroup, %v", err)
		return err
	}

	for _, v := range vnfGroups {
		if podEvent == DelPodEvent {
			if !util.ContainsString(v.Status.Ports, ovs.PodNameToPortName(podName, namespace)) {
				continue
			}
		} else if podEvent == AddPodEvent {
			if subnet != v.Spec.Subnet || !util.ContainsString(v.Spec.Ips, ip) {
				continue
			}
		}

		c.addOrUpdateVnfGroupQueue.Add(v.Name)
	}

	return nil
}
