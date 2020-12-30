package controller

import (
	"crypto/md5" // #nosec
	"encoding/json"
	"fmt"
	"reflect"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) runAddSfcWorker() {
	for c.processNextAddOrUpdateSfcWorkItem() {
	}
}

func (c *Controller) runDelSfcWorker() {
	for c.processNextDelSfcWorkItem() {
	}
}

func (c *Controller) enqueueAddSfc(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add sfc %s", key)
	c.addOrUpdateSfcQueue.Add(key)
}

func (c *Controller) enqueueUpdateSfc(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldSfc := old.(*kubeovnv1.Sfc)
	newSfc := new.(*kubeovnv1.Sfc)

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}

	if !newSfc.DeletionTimestamp.IsZero() {
		c.addOrUpdateSfcQueue.Add(key)
		return
	}

	if !reflect.DeepEqual(oldSfc.Spec.Subnet, newSfc.Spec.Subnet) ||
		!reflect.DeepEqual(oldSfc.Spec.VnfGroups, newSfc.Spec.VnfGroups) {
		klog.V(3).Infof("enqueue update sfc %s", key)
		c.addOrUpdateSfcQueue.Add(key)
	}
}

func (c *Controller) enqueueDelSfc(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add sfc %s", key)
	c.delSfcQueue.Add(key)
}

func (c *Controller) processNextAddOrUpdateSfcWorkItem() bool {
	obj, shutdown := c.addOrUpdateSfcQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOrUpdateSfcQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdateSfcQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOrUpdateSfc(key); err != nil {
			c.addOrUpdateSfcQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOrUpdateSfcQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDelSfcWorkItem() bool {
	obj, shutdown := c.delSfcQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delSfcQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delSfcQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelSfc(key); err != nil {
			c.delSfcQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delSfcQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddOrUpdateSfc(key string) error {
	c.sfcKeyMutex.Lock(key)
	defer c.sfcKeyMutex.Unlock(key)

	sfc, err := c.sfcLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	needUpdate := false
	if sfc.Status.Subnet != "" && sfc.Spec.Subnet != sfc.Status.Subnet {
		sfc.Spec.Subnet = sfc.Status.Subnet
		needUpdate = true

	}

	checkVgs := util.UniqString(sfc.Spec.VnfGroups)
	if len(checkVgs) != len(sfc.Spec.VnfGroups) {
		sfc.Spec.VnfGroups = checkVgs
		needUpdate = true
	}

	if needUpdate {
		if _, err = c.config.KubeOvnClient.KubeovnV1().Sfcs().Update(sfc); err != nil {
			return err
		}
		return nil
	}

	// create chain
	if err := c.ovnClient.CreatePortChain(sfc.Name, sfc.Spec.Subnet); err != nil {
		return err
	}

	var groupPorts []*kubeovnv1.VnfGroupPort
	for _, name := range sfc.Spec.VnfGroups {
		vnfGroup, err := c.vnfGroupLister.Get(name)
		if err != nil {
			klog.Warningf("get vnfGroup failed. %v", err)
			continue
		}
		if vnfGroup.Spec.Subnet != sfc.Spec.Subnet {
			continue
		}
		groupPorts = append(groupPorts, &kubeovnv1.VnfGroupPort{GroupName: name, Ports: vnfGroup.Status.Ports})
	}

	// check md5
	portsByte, err := json.Marshal(groupPorts)
	if err != nil {
		klog.Error(err)
	}

	md5Byte := md5.Sum(portsByte) // #nosec
	newMd5 := fmt.Sprintf("%x", md5Byte)
	klog.Infof("newMd5: %s", newMd5)
	if newMd5 == sfc.Status.Md5 {
		return nil
	}

	// rebuild port pair group
	if err := c.deletePortPairGroupOfChain(key); err != nil {
		return err
	}

	for index, vgName := range sfc.Spec.VnfGroups {
		portPairGroupName := genPortPairGroupName(sfc.Name, vgName)
		vnfGroup := getVnfGroupPortByName(vgName, groupPorts)
		if vnfGroup != nil {
			if err := c.ovnClient.CreatePortPairGroup(portPairGroupName, sfc.Name, index); err != nil {
				return err
			}

			for _, p := range vnfGroup.Ports {
				if err := c.ovnClient.AddPortPairToGroup(portPairGroupName, genPortPairName(vgName, p)); err != nil {
					return err
				}
			}
		}
	}

	sfc.Status.Md5 = newMd5
	sfc.Status.Subnet = sfc.Spec.Subnet
	sfc.Status.ChainExist = true
	sfc.Status.PortRecords = groupPorts
	err = c.reconcilePodClassifier(sfc.Name)
	if err != nil {
		klog.Errorf("reconcile pod classifier failed. %v", err)
		return err
	}

	return c.patchSfcStatus(sfc)
}

func (c Controller) patchSfcStatus(sfc *kubeovnv1.Sfc) error {
	bytes, err := sfc.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().Sfcs().Patch(sfc.Name, types.MergePatchType, bytes, "status")
	if err != nil {
		return err
	}
	return nil
}

func genPortPairGroupName(sfcName, vnfName string) string {
	return fmt.Sprintf("%s-ppg-%s", sfcName, vnfName)
}

func (c *Controller) reconcileSfcs(vnfGroup *kubeovnv1.VnfGroup) error {
	sfcs, err := c.sfcLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list sfc, %v", err)
		return err
	}

	for _, sfc := range sfcs {
		if !sfc.Status.ChainExist || vnfGroup.Spec.Subnet != sfc.Spec.Subnet || !util.ContainsString(sfc.Spec.VnfGroups, vnfGroup.Name) {
			continue
		}

		pps := getVnfGroupPortByName(vnfGroup.Name, sfc.Status.PortRecords)
		if pps != nil && len(util.DiffStringSlice(pps.Ports, vnfGroup.Status.Ports)) != 0 {
			continue
		}

		c.addOrUpdateSfcQueue.Add(sfc.Name)
	}
	return nil
}

func (c *Controller) handleDelSfc(key string) error {
	if err := c.reconcilePodClassifier(key); err != nil {
		return err
	}

	if err := c.deletePortPairGroupOfChain(key); err != nil {
		return err
	}

	if err := c.ovnClient.DelPortChain(key); err != nil {
		return err
	}
	return nil
}

func (c *Controller) deletePortPairGroupOfChain(chainName string) error {
	ppgs, err := c.ovnClient.ListLogicalPortPairGroup(chainName)
	if err != nil {
		return err
	}
	for _, ppg := range ppgs {
		if err := c.ovnClient.DelLogicalPortPairGroup(ppg); err != nil {
			return err
		}
	}
	return nil
}

func getVnfGroupPortByName(name string, portRecords []*kubeovnv1.VnfGroupPort) *kubeovnv1.VnfGroupPort {
	for _, n := range portRecords {
		if n.GroupName == name {
			return n
		}
	}
	return nil
}

func (c *Controller) cleanPortPairGroup(chainName string) error {
	// clean port pair group
	ppgs, err := c.ovnClient.ListLogicalPortPairGroup(chainName)
	if err != nil {
		return err
	}
	for _, ppg := range ppgs {
		if err = c.ovnClient.DelLogicalPortPairGroup(ppg); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) getSfcWithVnfGroup(vnfGroupName string) (sfcNames []string, err error) {
	sfcs, err := c.sfcLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, sfc := range sfcs {
		for _, vnfGroupPort := range sfc.Status.PortRecords {
			if vnfGroupPort.GroupName == vnfGroupName {
				sfcNames = append(sfcNames, sfc.Name)
			}
		}
	}
	return
}
