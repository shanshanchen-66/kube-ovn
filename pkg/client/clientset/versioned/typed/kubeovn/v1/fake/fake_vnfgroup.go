/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeVnfGroups implements VnfGroupInterface
type FakeVnfGroups struct {
	Fake *FakeKubeovnV1
}

var vnfgroupsResource = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vnfgroups"}

var vnfgroupsKind = schema.GroupVersionKind{Group: "kubeovn.io", Version: "v1", Kind: "VnfGroup"}

// Get takes name of the vnfGroup, and returns the corresponding vnfGroup object, and an error if there is any.
func (c *FakeVnfGroups) Get(name string, options v1.GetOptions) (result *kubeovnv1.VnfGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(vnfgroupsResource, name), &kubeovnv1.VnfGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeovnv1.VnfGroup), err
}

// List takes label and field selectors, and returns the list of VnfGroups that match those selectors.
func (c *FakeVnfGroups) List(opts v1.ListOptions) (result *kubeovnv1.VnfGroupList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(vnfgroupsResource, vnfgroupsKind, opts), &kubeovnv1.VnfGroupList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &kubeovnv1.VnfGroupList{ListMeta: obj.(*kubeovnv1.VnfGroupList).ListMeta}
	for _, item := range obj.(*kubeovnv1.VnfGroupList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested vnfGroups.
func (c *FakeVnfGroups) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(vnfgroupsResource, opts))
}

// Create takes the representation of a vnfGroup and creates it.  Returns the server's representation of the vnfGroup, and an error, if there is any.
func (c *FakeVnfGroups) Create(vnfGroup *kubeovnv1.VnfGroup) (result *kubeovnv1.VnfGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(vnfgroupsResource, vnfGroup), &kubeovnv1.VnfGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeovnv1.VnfGroup), err
}

// Update takes the representation of a vnfGroup and updates it. Returns the server's representation of the vnfGroup, and an error, if there is any.
func (c *FakeVnfGroups) Update(vnfGroup *kubeovnv1.VnfGroup) (result *kubeovnv1.VnfGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(vnfgroupsResource, vnfGroup), &kubeovnv1.VnfGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeovnv1.VnfGroup), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeVnfGroups) UpdateStatus(vnfGroup *kubeovnv1.VnfGroup) (*kubeovnv1.VnfGroup, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(vnfgroupsResource, "status", vnfGroup), &kubeovnv1.VnfGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeovnv1.VnfGroup), err
}

// Delete takes name of the vnfGroup and deletes it. Returns an error if one occurs.
func (c *FakeVnfGroups) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(vnfgroupsResource, name), &kubeovnv1.VnfGroup{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeVnfGroups) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(vnfgroupsResource, listOptions)

	_, err := c.Fake.Invokes(action, &kubeovnv1.VnfGroupList{})
	return err
}

// Patch applies the patch and returns the patched vnfGroup.
func (c *FakeVnfGroups) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *kubeovnv1.VnfGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(vnfgroupsResource, name, pt, data, subresources...), &kubeovnv1.VnfGroup{})
	if obj == nil {
		return nil, err
	}
	return obj.(*kubeovnv1.VnfGroup), err
}
