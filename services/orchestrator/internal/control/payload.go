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

type createAppPayload struct {
	ScenarioTemplateID string `json:"scenarioTemplateId"`
	InstanceName       string `json:"instanceName"`
	PerformancePercent int    `json:"performancePercent,omitempty"`
}

type instancePayload struct {
	InstanceName string `json:"instanceName"`
}

type updateCapacityPayload struct {
	ScenarioTemplateID string `json:"scenarioTemplateId"`
	InstanceName       string `json:"instanceName"`
	PerformancePercent int    `json:"performancePercent"`
}

type upstreamPayload struct {
	Servers []struct {
		Host   string `json:"host"`
		Port   int    `json:"port"`
		Weight int    `json:"weight"`
	} `json:"servers"`
}

func (p upstreamPayload) nginxServers() []nginx.Server {
	servers := make([]nginx.Server, 0, len(p.Servers))
	for _, server := range p.Servers {
		servers = append(servers, nginx.Server{
			Host: server.Host, Port: server.Port, Weight: server.Weight,
		})
	}
	return servers
}

type reconcilePayload struct {
	ExpectedLabIDs *[]string `json:"expectedLabIds"`
	Cleanup        bool      `json:"cleanup"`
}
