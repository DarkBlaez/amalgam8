package checker

import (
	"strconv"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/amalgam8/controller/resources"
	"github.com/amalgam8/sidecar/router/clients"
	"github.com/amalgam8/sidecar/router/nginx"
)

type Listener interface {
	CatalogChange(catalog resources.ServiceCatalog) error
	RulesChange(proxyConfig resources.ProxyConfig) error
}

type listener struct {
	catalog     resources.ServiceCatalog
	proxyConfig resources.ProxyConfig
	nginx       nginx.Nginx
	mutex       sync.Mutex
}

func NewListener(nginxClient nginx.Nginx) Listener {
	return &listener{
		proxyConfig: resources.ProxyConfig{
			LoadBalance: "round_robin",
			Filters: resources.Filters{
				Versions: []resources.Version{},
				Rules:    []resources.Rule{},
			},
		},
		nginx: nginxClient,
	}
}

func (l *listener) CatalogChange(catalog resources.ServiceCatalog) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.catalog = catalog
	return l.updateNGINX()
}

func (l *listener) RulesChange(proxyConfig resources.ProxyConfig) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.proxyConfig = proxyConfig
	return l.updateNGINX()
}

func (l *listener) updateNGINX() error {
	nginxJSON := l.buildConfig()

	return l.nginx.Update(nginxJSON)

}

func (l *listener) buildConfig() clients.NGINXJson {

	retval := clients.NGINXJson{
		Upstreams: make(map[string]clients.NGINXUpstream, 0),
		Services:  make(map[string]clients.NGINXService, 0),
	}
	faults := []clients.NGINXFault{}
	for _, rule := range l.proxyConfig.Filters.Rules {
		fault := clients.NGINXFault{
			Delay:            rule.Delay,
			DelayProbability: rule.DelayProbability,
			AbortProbability: rule.AbortProbability,
			AbortCode:        rule.ReturnCode,
			Source:           rule.Source,
			Destination:      rule.Destination,
			Header:           rule.Header,
			Pattern:          rule.Pattern,
		}
		faults = append(faults, fault)
	}
	retval.Faults = faults

	types := map[string]string{}
	for _, service := range l.catalog.Services {
		upstreams := map[string][]clients.NGINXEndpoint{}
		for _, endpoint := range service.Endpoints {
			version := endpoint.Metadata.Version
			upstreamName := service.Name
			if version != "" {
				upstreamName += ":" + version
			} else {
				upstreamName += ":" + "UNVERSIONED"
			}

			types[service.Name] = endpoint.Type

			vals := strings.Split(endpoint.Value, ":")
			if len(vals) != 2 {
				logrus.WithFields(logrus.Fields{
					"endpoint": endpoint,
					"values":   vals,
				}).Error("could not parse host and port from service endpoint")
			}
			host := vals[0]
			port, err := strconv.Atoi(vals[1])
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"err":  err,
					"port": vals[1],
				}).Error("port not a valid int")
			}

			versionUpstreams := upstreams[upstreamName]
			nginxEndpoint := clients.NGINXEndpoint{
				Host: host,
				Port: port,
			}
			if versionUpstreams == nil {
				versionUpstreams = []clients.NGINXEndpoint{nginxEndpoint}
			} else {
				versionUpstreams = append(versionUpstreams, nginxEndpoint)
			}
			upstreams[upstreamName] = versionUpstreams
		}

		for k, v := range upstreams {
			retval.Upstreams[k] = clients.NGINXUpstream{
				Upstreams: v,
			}
		}
	}

	versions := map[string]resources.Version{}
	for _, version := range l.proxyConfig.Filters.Versions {
		versions[version.Service] = version
	}

	for k, v := range types {
		if version, ok := versions[k]; ok {
			retval.Services[k] = clients.NGINXService{
				Default:   version.Default,
				Selectors: version.Selectors,
				Type:      v,
			}
		} else {
			retval.Services[k] = clients.NGINXService{
				Type: v,
			}
		}
	}

	return retval
}
