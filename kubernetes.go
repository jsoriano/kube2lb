/*
Copyright 2016 Tuenti Technologies S.L. All rights reserved.

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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var defaultPortMode = "http"

func init() {
	flag.StringVar(&defaultPortMode, "default-port-mode", defaultPortMode, "Default mode for service ports")
}

type KubernetesClient struct {
	config    *rest.Config
	clientset *kubernetes.Clientset

	nodesIndexer     cache.Indexer
	servicesIndexer  cache.Indexer
	endpointsIndexer cache.Indexer

	controllersStop chan struct{}

	notifiers []Notifier
	templates []*Template

	domain string
}

const (
	ExternalDomainsAnnotation = "kube2lb/external-domains"
	PortModeAnnotation        = "kube2lb/port-mode"
)

func NewKubernetesClient(kubecfg, apiserver, domain string) (*KubernetesClient, error) {
	config, err := clientcmd.BuildConfigFromFlags(apiserver, kubecfg)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	kc := &KubernetesClient{
		config:    config,
		clientset: clientset,
		notifiers: make([]Notifier, 0, 10),
		templates: make([]*Template, 0, 10),
		domain:    domain,
	}
	return kc, nil
}

func (c *KubernetesClient) AddNotifier(n Notifier) {
	c.notifiers = append(c.notifiers, n)
}

func (c *KubernetesClient) Notify() {
	for _, n := range c.notifiers {
		if err := n.Notify(); err != nil {
			log.Printf("Couldn't notify: %s", err)
		}
	}
}

func (c *KubernetesClient) AddTemplate(t *Template) {
	c.templates = append(c.templates, t)
}

func (c *KubernetesClient) ExecuteTemplates(info *ClusterInformation) {
	for _, t := range c.templates {
		if err := t.Execute(info); err != nil {
			log.Printf("Couldn't write template: %s", err)
		}
	}
}

func (c *KubernetesClient) getNodeNames() ([]string, error) {
	lister := cache.StoreToNodeLister{c.nodesIndexer}
	nodes, err := lister.List()
	if err != nil {
		return nil, err
	}
	nodeNames := make([]string, len(nodes.Items))
	for i, n := range nodes.Items {
		nodeNames[i] = n.Name
	}
	return nodeNames, nil
}

func (c *KubernetesClient) getServices(namespace string) ([]ServiceInformation, error) {
	servicesLister := cache.StoreToServiceLister{c.servicesIndexer}
	services, err := servicesLister.List(nil)
	if err != nil {
		return nil, fmt.Errorf("Couldn't get services: %s", err)
	}

	endpointsLister := cache.StoreToEndpointsLister{c.endpointsIndexer}
	endpoints, err := endpointsLister.List()
	if err != nil {
		return nil, fmt.Errorf("Couldn't get endpoints: %s", err)
	}

	endpointsHelper := NewEndpointsHelper(&endpoints)

	servicesInformation := make([]ServiceInformation, 0, len(services))
	for _, s := range services {
		var external []string
		if domains, ok := s.ObjectMeta.Annotations[ExternalDomainsAnnotation]; ok && len(domains) > 0 {
			external = strings.Split(domains, ",")
		}
		var portModes map[string]string
		if modes, ok := s.ObjectMeta.Annotations[PortModeAnnotation]; ok && len(modes) > 0 {
			err := json.Unmarshal([]byte(modes), &portModes)
			if err != nil {
				log.Printf("Couldn't parse %s annotation for %s service", PortModeAnnotation, s.Name)
			}
		}

		switch s.Spec.Type {
		case api.ServiceTypeNodePort, api.ServiceTypeLoadBalancer:
			endpointsPortsMap := endpointsHelper.ServicePortsMap(s)
			if len(endpointsPortsMap) == 0 {
				log.Printf("Couldn't find endpoints for %s in %s?", s.Name, s.Namespace)
				continue
			}

			for _, port := range s.Spec.Ports {
				mode, ok := portModes[port.Name]
				if !ok {
					mode = defaultPortMode
				}
				servicesInformation = append(servicesInformation,
					ServiceInformation{
						Name:      s.Name,
						Namespace: s.Namespace,
						Port: PortSpec{
							port.Port,
							strings.ToLower(mode),
							strings.ToLower(string(port.Protocol)),
						},
						Endpoints: endpointsPortsMap[port.TargetPort.IntVal],
						NodePort:  port.NodePort,
						External:  external,
					},
				)
			}
		}
	}
	return servicesInformation, nil
}

func (c *KubernetesClient) Update() error {
	nodeNames, err := c.getNodeNames()
	if err != nil {
		return fmt.Errorf("Couldn't get nodes: %s", err)
	}

	services, err := c.getServices(api.NamespaceAll)
	if err != nil {
		return fmt.Errorf("Couldn't get services: %s", err)
	}

	portsMap := make(map[PortSpec]bool)
	for _, service := range services {
		portsMap[service.Port] = true
	}
	ports := make([]PortSpec, 0, len(portsMap))
	for port := range portsMap {
		ports = append(ports, port)
	}

	info := &ClusterInformation{
		Nodes:    nodeNames,
		Services: services,
		Ports:    ports,
		Domain:   c.domain,
	}
	c.ExecuteTemplates(info)
	c.Notify()

	return nil
}

func (c *KubernetesClient) Watch() error {
	log.Printf("Using %s for kubernetes master", c.config.Host)

	var controllers []cache.ControllerInterface

	isFirstUpdate := true
	updater := NewUpdater(func() {
		if len(controllers) == 0 {
			return
		}
		var err error
		if err = c.Update(); err != nil {
			log.Printf("Couldn't update state: %s", err)
		}
		if isFirstUpdate {
			if err != nil {
				log.Fatalf("Failing on first update, check configuration.")
			}
			isFirstUpdate = false
		}
	})
	go updater.Run()

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(object interface{}) { updater.Signal() },
		UpdateFunc: func(oldObject, newObject interface{}) { updater.Signal() },
		DeleteFunc: func(object interface{}) { updater.Signal() },
	}

	nodesIndexer, nodesController := cache.NewIndexerInformer(
		cache.NewListWatchFromClient(c.clientset.Core().RESTClient(), "node", api.NamespaceAll, nil),
		&api.Node{}, 0, handler, nil)
	servicesIndexer, servicesController := cache.NewIndexerInformer(
		cache.NewListWatchFromClient(c.clientset.Core().RESTClient(), "service", api.NamespaceAll, nil),
		&api.Service{}, 0, handler, nil)
	endpointsIndexer, endpointsController := cache.NewIndexerInformer(
		cache.NewListWatchFromClient(c.clientset.Core().RESTClient(), "endpoints", api.NamespaceAll, nil),
		&api.Endpoints{}, 0, handler, nil)

	c.nodesIndexer = nodesIndexer
	c.servicesIndexer = servicesIndexer
	c.endpointsIndexer = endpointsIndexer

	controllers = []cache.ControllerInterface{
		nodesController,
		servicesController,
		endpointsController,
	}

	stop := make(chan struct{}, len(controllers))
	stopped := make(chan struct{}, len(controllers))
	for _, controller := range controllers {
		go func() {
			controller.Run(stop)
			stopped <- struct{}{}
		}()
	}

	c.controllersStop = make(chan struct{}, 1)
	go func() {
		<-c.controllersStop
		for range controllers {
			stop <- struct{}{}
		}
	}()

	for range controllers {
		<-stopped
	}

	return nil
}
