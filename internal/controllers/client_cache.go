package controllers

import (
	"sync"

	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClientCache struct {
	cache map[string]RemoteClusterClients
	mu    sync.RWMutex
}

func NewClientCache() *ClientCache {
	return &ClientCache{
		cache: map[string]RemoteClusterClients{},
	}
}

type clientCache interface {
	Free(id string)
	Set(
		id string,
		host string,
		client client.Client,
		discoveryClient discovery.DiscoveryInterface,
	)
	Get(id string) (
		host string,
		client client.Client,
		discoveryClient discovery.DiscoveryInterface,
		ok bool,
	)
}

type RemoteClusterClients struct {
	Host            string
	Client          client.Client
	DiscoveryClient discovery.DiscoveryInterface
}

func (c *ClientCache) Free(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, id)
}

func (c *ClientCache) Set(
	id string,
	host string,
	client client.Client,
	discoveryClient discovery.DiscoveryInterface,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[id] = RemoteClusterClients{
		Host:            host,
		Client:          client,
		DiscoveryClient: discoveryClient,
	}
}

func (c *ClientCache) Get(id string) (
	host string,
	client client.Client,
	discoveryClient discovery.DiscoveryInterface,
	ok bool,
) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.cache[id]
	if !ok {
		return "", nil, nil, false
	}
	return entry.Host, entry.Client, entry.DiscoveryClient, true
}
