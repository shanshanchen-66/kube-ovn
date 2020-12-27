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
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
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
	klog.V(3).Infof("enqueue add vnfGroup %s", key)
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

	if !newVnf.DeletionTimestamp.IsZero() {
		c.addOrUpdateVnfGroupQueue.Add(key)
		return
	}

	if !reflect.DeepEqual(oldVnf.Spec.Subnet, newVnf.Spec.Subnet) ||
		!reflect.DeepEqual(oldVnf.Spec.Ips, newVnf.Spec.Ips) {
		klog.V(3).Infof("enqueue update vnf %s", key)
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
	klog.V(3).Infof("enqueue add vnf %s", key)
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
		var vnfGroupName string
		var ok bool
		if vnfGroupName, ok = obj.(string); !ok {
			c.delVnfGroupQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelVnfGroup(vnfGroupName); err != nil {
			c.delVnfGroupQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", vnfGroupName, err.Error())
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
	vnfGroup, err := c.vnfGroupLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	needUpdate := false
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

	var newVnfPorts []string
	subnet := c.ipam.Subnets[vnfGroup.Spec.Subnet]
	for _, ip := range vnfGroup.Spec.Ips {
		if podKey, ok := subnet.IPToPod[ipam.IP(ip)]; ok {
			c.podKeyMutex.Lock(podKey)
			defer c.podKeyMutex.Unlock(podKey)
			namespace, name, err := cache.SplitMetaNamespaceKey(podKey)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("invalid pod key: %s", podKey))
				return nil
			}
			pod, err := c.podsLister.Pods(namespace).Get(name)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			newVnfPorts = append(newVnfPorts, ovs.PodNameToPortName(pod.Name, pod.Namespace))
		}
	}

	if len(newVnfPorts) == len(vnfGroup.Status.Ports) && len(util.DiffStringSlice(newVnfPorts, vnfGroup.Status.Ports)) == 0 {
		return nil
	}

	// clean invalid port pair
	for _, port := range vnfGroup.Status.Ports {
		if !util.ContainsString(newVnfPorts, port) {
			if err = c.ovnClient.DelLogicalPortPair(genPortPairName(vnfGroup.Name, port)); err != nil {
				return err
			}
		}
	}

	// create new port pair
	for _, port := range newVnfPorts {
		if !util.ContainsString(vnfGroup.Status.Ports, port) {
			if err = c.ovnClient.CreatePortPair(genPortPairName(vnfGroup.Name, port), vnfGroup.Spec.Subnet, port, port); err != nil {
				return err
			}
		}

	}

	if err := c.reconcileSfcs(vnfGroup); err != nil {
		return err
	}

	vnfGroup.Status.Ports = newVnfPorts
	bytes, err := vnfGroup.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().VnfGroups().Patch(vnfGroup.Name, types.MergePatchType, bytes, "status")
	if err != nil {
		return err
	}

	return nil
}

func genPortPairName(vnfGroupName, port string) string {
	return fmt.Sprintf("%s-pp-%s", vnfGroupName, port)
}

func (c *Controller) handleDelVnfGroup(key string) error {
	klog.Infof("start to gc port pair")
	pps, err := c.ovnClient.ListLogicalPortPair()
	if err != nil {
		klog.Errorf("failed to list port pair, %v", err)
		return err
	}

	for _, pp := range pps {
		result := strings.Split(pp, "-pp-")
		if len(result) != 2 || result[0] == key {
			if err := c.ovnClient.DelLogicalPortPair(pp); err != nil {
				klog.Errorf("failed to delete port pair %s , %v", pp, err)
				return err
			}
		}
	}
	return nil
}
