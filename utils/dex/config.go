package dex

import (
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/common"
	"github.com/argoproj/argo-rollouts/utils/settings"
)

func GenerateDexConfigYAML(argoRolloutsSettings *settings.ArgoRolloutsSettings, disableTls bool) ([]byte, error) {
	if !argoRolloutsSettings.IsDexConfigured() {
		return nil, nil
	}
	redirectURL, err := argoRolloutsSettings.RedirectURL()
	if err != nil {
		return nil, fmt.Errorf("failed to infer redirect url from config: %w", err)
	}
	var dexCfg map[string]interface{}
	err = yaml.Unmarshal([]byte(argoRolloutsSettings.DexConfig), &dexCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal dex.config from configmap: %w", err)
	}
	dexCfg["issuer"] = argoRolloutsSettings.IssuerURL()
	dexCfg["storage"] = map[string]interface{}{
		"type": "memory",
	}
	if disableTls {
		dexCfg["web"] = map[string]interface{}{
			"http": "0.0.0.0:5556",
		}
	} else {
		dexCfg["web"] = map[string]interface{}{
			"https":   "0.0.0.0:5556",
			"tlsCert": "/tmp/tls.crt",
			"tlsKey":  "/tmp/tls.key",
		}
	}

	dexCfg["grpc"] = map[string]interface{}{
		"addr": "0.0.0.0:5557",
	}
	dexCfg["telemetry"] = map[string]interface{}{
		"http": "0.0.0.0:5558",
	}

	if oauth2Cfg, found := dexCfg["oauth2"].(map[string]interface{}); found {
		if _, found := oauth2Cfg["skipApprovalScreen"].(bool); !found {
			oauth2Cfg["skipApprovalScreen"] = true
		}
	} else {
		dexCfg["oauth2"] = map[string]interface{}{
			"skipApprovalScreen": true,
		}
	}

	argoRolloutsStaticClient := map[string]interface{}{
		"id":     common.ArgoRolloutsClientAppID,
		"name":   common.ArgoRolloutsClientAppName,
		"secret": argoRolloutsSettings.DexOAuth2ClientSecret(),
		"redirectURIs": []string{
			redirectURL,
		},
	}
	argoRolloutsPKCEStaticClient := map[string]interface{}{
		"id":   "argo-cd-pkce",
		"name": "Argo CD PKCE",
		"redirectURIs": []string{
			"http://localhost:4000/pkce/verify",
		},
		"public": true,
	}
	argoRolloutsCLIStaticClient := map[string]interface{}{
		"id":     common.ArgoRolloutsCLIClientAppID,
		"name":   common.ArgoRolloutsCLIClientAppName,
		"public": true,
		"redirectURIs": []string{
			"http://localhost",
			"http://localhost:8085/auth/callback",
		},
	}

	staticClients, ok := dexCfg["staticClients"].([]interface{})
	if ok {
		dexCfg["staticClients"] = append([]interface{}{argoRolloutsStaticClient, argoRolloutsCLIStaticClient, argoRolloutsPKCEStaticClient}, staticClients...)
	} else {
		dexCfg["staticClients"] = []interface{}{argoRolloutsStaticClient, argoRolloutsCLIStaticClient, argoRolloutsPKCEStaticClient}
	}

	dexRedirectURL, err := argoRolloutsSettings.DexRedirectURL()
	if err != nil {
		return nil, err
	}
	connectors, ok := dexCfg["connectors"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("malformed Dex configuration found")
	}
	for i, connectorIf := range connectors {
		connector, ok := connectorIf.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("malformed Dex configuration found")
		}
		connectorType := connector["type"].(string)
		if !needsRedirectURI(connectorType) {
			continue
		}
		connectorCfg, ok := connector["config"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("malformed Dex configuration found")
		}
		connectorCfg["redirectURI"] = dexRedirectURL
		connector["config"] = connectorCfg
		connectors[i] = connector
	}
	dexCfg["connectors"] = connectors
	dexCfg = settings.ReplaceMapSecrets(dexCfg, argoRolloutsSettings.Secrets)
	return yaml.Marshal(dexCfg)
}

// needsRedirectURI returns whether or not the given connector type needs a redirectURI
// Update this list as necessary, as new connectors are added
// https://dexidp.io/docs/connectors/
func needsRedirectURI(connectorType string) bool {
	switch connectorType {
	case "oidc", "saml", "microsoft", "linkedin", "gitlab", "github", "bitbucket-cloud", "openshift", "gitea", "google", "oauth":
		return true
	}
	return false
}
