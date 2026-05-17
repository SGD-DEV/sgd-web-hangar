package mcp

import (
	"encoding/json"
	"fmt"
)

type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

func (s *Server) getResourceDefinitions() []ResourceDefinition {
	return []ResourceDefinition{
		{
			URI:         "devour://status",
			Name:        "Service Status",
			Description: "Current status of all managed services",
			MimeType:    "application/json",
		},
		{
			URI:         "devour://projects",
			Name:        "Projects",
			Description: "List of all registered projects",
			MimeType:    "application/json",
		},
		{
			URI:         "devour://php/versions",
			Name:        "PHP Versions",
			Description: "List of installed PHP versions",
			MimeType:    "application/json",
		},
	}
}

func (s *Server) handleResourcesList(req JSONRPCRequest) (interface{}, *RPCError) {
	return map[string]interface{}{
		"resources": s.getResourceDefinitions(),
	}, nil
}

func (s *Server) handleResourcesRead(req JSONRPCRequest) (interface{}, *RPCError) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	switch params.URI {
	case "devour://status":
		return s.resourceStatus()
	case "devour://projects":
		return s.resourceProjects()
	case "devour://php/versions":
		return s.resourcePHPVersions()
	default:
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("Unknown resource: %s", params.URI)}
	}
}

func (s *Server) resourceStatus() (interface{}, *RPCError) {
	statuses := s.serviceManager.AllStatuses()
	data, _ := json.Marshal(statuses)
	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      "devour://status",
				"mimeType": "application/json",
				"text":     string(data),
			},
		},
	}, nil
}

func (s *Server) resourceProjects() (interface{}, *RPCError) {
	projects := s.projectManager.List()
	data, _ := json.Marshal(projects)
	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      "devour://projects",
				"mimeType": "application/json",
				"text":     string(data),
			},
		},
	}, nil
}

func (s *Server) resourcePHPVersions() (interface{}, *RPCError) {
	versions := s.phpManager.ListInstalled()
	data, _ := json.Marshal(versions)
	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      "devour://php/versions",
				"mimeType": "application/json",
				"text":     string(data),
			},
		},
	}, nil
}
