/*
Copyright 2017 Tuenti Technologies S.L. All rights reserved.

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

package main

import (
	"fmt"

	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/runtime"
)

const resourceLinkFmt = "/api/v1/namespaces/%s/%s/%s"

func resourceLink(namespace, resource, name string) string {
	return fmt.Sprintf(resourceLinkFmt, namespace, resource, name)
}

type Store interface {
	Delete(runtime.Object) runtime.Object
	Update(runtime.Object) runtime.Object
}

type LocalStore struct {
	Objects map[string]runtime.Object
}

func NewLocalStore() *LocalStore {
	return &LocalStore{
		Objects: make(map[string]runtime.Object),
	}
}

func (s *LocalStore) Update(o runtime.Object) runtime.Object {
	accessor, _ := meta.Accessor(o)
	link := accessor.GetSelfLink()
	old := s.Objects[link]
	s.Objects[link] = o
	return old
}

func (s *LocalStore) Delete(o runtime.Object) runtime.Object {
	accessor, _ := meta.Accessor(o)
	link := accessor.GetSelfLink()
	old := s.Objects[link]
	delete(s.Objects, link)
	return old
}

type NodeStore struct {
	*LocalStore
}

func (s *NodeStore) GetNames() []string {
	var nodeNames []string
	for _, o := range s.Objects {
		accessor, _ := meta.Accessor(o)
		nodeNames = append(nodeNames, accessor.GetName())
	}
	return nodeNames
}

type ServiceStore struct {
	*LocalStore
}

func (s *ServiceStore) List() ([]*v1.Service, error) {
	var services []*v1.Service
	for _, o := range s.Objects {
		service, ok := o.(*v1.Service)
		if !ok {
			return nil, fmt.Errorf("couldn't convert service")
		}
		services = append(services, service)
	}
	return services, nil
}

func (s *ServiceStore) Get(namespace, name string) (*v1.Service, bool) {
	link := resourceLink(namespace, "services", name)
	o, found := s.Objects[link]
	if !found {
		return nil, false
	}
	service, ok := o.(*v1.Service)
	if !ok {
		return nil, false
	}
	return service, true
}

type EndpointsStore struct {
	*LocalStore
}

func (s *EndpointsStore) List() ([]*v1.Endpoints, error) {
	var endpoints []*v1.Endpoints
	for _, o := range s.Objects {
		endpoint, ok := o.(*v1.Endpoints)
		if !ok {
			return nil, fmt.Errorf("couldn't convert endpoints")
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}
