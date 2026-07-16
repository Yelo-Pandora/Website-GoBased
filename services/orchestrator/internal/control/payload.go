package control

import "website-gobased/services/orchestrator/internal/nginx"

type provisionPayload struct {
	CourseID           uint64 `json:"courseId"`
	ScenarioType       string `json:"scenarioType"`
	ScenarioTemplateID string `json:"scenarioTemplateId"`
	InitialInstances   int    `json:"initialInstances"`
	RedisRequired      bool   `json:"redisRequired"`
}

type scenarioPayload struct {
	ScenarioTemplateID string `json:"scenarioTemplateId"`
}

type destroyPayload struct {
	Reason string `json:"reason,omitempty"`
}

type createAppPayload struct {
	ScenarioTemplateID string `json:"scenarioTemplateId"`
	InstanceName       string `json:"instanceName"`
	PerformancePercent int    `json:"performancePercent,omitempty"`
}

type instancePayload struct {
	InstanceName string `json:"instanceName"`
}

type updateCapacityPayload struct {
	ScenarioTemplateID         string `json:"scenarioTemplateId"`
	InstanceName               string `json:"instanceName"`
	PerformancePercent         int    `json:"performancePercent"`
	PreviousPerformancePercent int    `json:"previousPerformancePercent"`
}

type upstreamPayload struct {
	Servers []struct {
		InstanceName string `json:"instanceName"`
		Weight       int    `json:"weight"`
	} `json:"servers"`
}

func (p upstreamPayload) nginxServers(names resourceNames) ([]nginx.Server, error) {
	servers := make([]nginx.Server, 0, len(p.Servers))
	for _, server := range p.Servers {
		if !validInstanceName(server.InstanceName) || server.Weight <= 0 || server.Weight > 100 {
			return nil, errInvalidUpstream
		}
		servers = append(servers, nginx.Server{
			Host: names.appContainer(server.InstanceName), Port: 8080, Weight: server.Weight,
		})
	}
	return servers, nil
}

type reconcilePayload struct {
	ExpectedLabIDs *[]string `json:"expectedLabIds"`
	Cleanup        bool      `json:"cleanup"`
}
