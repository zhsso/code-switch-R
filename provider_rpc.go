package main

import (
	"fmt"
	"net/url"

	"codeswitch/services"
)

const maskedURLValue = "redacted"

// providerRPCView shadows Provider.APIKey at the Web boundary. Provider data
// stays reusable inside the relay while ordinary browser reads never receive a
// cleartext credential.
type providerRPCView struct {
	services.Provider
	APIKey           string `json:"apiKey"`
	APIKeyConfigured bool   `json:"apiKeyConfigured"`
	APIKeyChanged    bool   `json:"apiKeyChanged,omitempty"`
}

type providerRPCSnapshot struct {
	Providers   []providerRPCView     `json:"providers"`
	ModelGroups []services.ModelGroup `json:"modelGroups"`
	Generation  int64                 `json:"generation"`
}

type providerRPCService struct {
	providers *services.ProviderService
}

func newProviderRPCService(providers *services.ProviderService) *providerRPCService {
	return &providerRPCService{providers: providers}
}

func (service *providerRPCService) LoadProviders(kind string) (providerRPCSnapshot, error) {
	providers, groups, generation, err := service.providers.LoadConfigurationWithGen(kind)
	if err != nil {
		return providerRPCSnapshot{}, err
	}
	return providerRPCSnapshot{Providers: maskProviders(providers), ModelGroups: groups, Generation: generation}, nil
}

func (service *providerRPCService) SaveProviders(kind string, generation int64, views []providerRPCView) (int64, error) {
	_, nextGeneration, err := service.providers.UpdateProviders(kind, generation, func(current []services.Provider) ([]services.Provider, error) {
		return mergeProviderViews(current, views), nil
	})
	return nextGeneration, err
}

func (service *providerRPCService) SaveModelGroups(kind string, generation int64, groups []services.ModelGroup) (int64, error) {
	return service.providers.UpdateModelGroups(kind, generation, groups)
}

func mergeProviderViews(current []services.Provider, views []providerRPCView) []services.Provider {
	currentByID := make(map[services.ProviderID]services.Provider, len(current))
	for _, provider := range current {
		currentByID[provider.ID] = provider
	}

	providers := make([]services.Provider, len(views))
	for index, view := range views {
		provider := view.Provider
		provider.APIKey = view.APIKey
		if existing, ok := currentByID[provider.ID]; ok {
			if !view.APIKeyChanged {
				provider.APIKey = existing.APIKey
			}
			provider.APIURL = restoreMaskedURL(provider.APIURL, existing.APIURL)
			provider.APIEndpoint = restoreMaskedURL(provider.APIEndpoint, existing.APIEndpoint)
			provider.FallbackAPIURLs = restoreMaskedURLs(provider.FallbackAPIURLs, existing.FallbackAPIURLs)
		}
		providers[index] = provider
	}
	return providers
}

func (service *providerRPCService) RevealProviderAPIKey(kind string, id services.ProviderID) (string, error) {
	providers, err := service.providers.LoadProviders(kind)
	if err != nil {
		return "", err
	}
	for _, provider := range providers {
		if provider.ID == id {
			return provider.APIKey, nil
		}
	}
	return "", fmt.Errorf("provider %s not found", id)
}

func (service *providerRPCService) DuplicateProvider(kind string, sourceID services.ProviderID) (*providerRPCView, error) {
	provider, err := service.providers.DuplicateProvider(kind, sourceID)
	if err != nil || provider == nil {
		return nil, err
	}
	view := maskProvider(*provider)
	return &view, nil
}

func (service *providerRPCService) RenameProvider(kind string, id services.ProviderID, name string) (int64, error) {
	return service.providers.RenameProviderWithGeneration(kind, id, name)
}

func maskProviders(providers []services.Provider) []providerRPCView {
	views := make([]providerRPCView, len(providers))
	for index, provider := range providers {
		views[index] = maskProvider(provider)
	}
	return views
}

func maskProvider(provider services.Provider) providerRPCView {
	configured := provider.APIKey != ""
	provider.APIKey = ""
	provider.APIURL = maskURLCredentials(provider.APIURL)
	provider.APIEndpoint = maskURLCredentials(provider.APIEndpoint)
	for index, fallback := range provider.FallbackAPIURLs {
		provider.FallbackAPIURLs[index] = maskURLCredentials(fallback)
	}
	return providerRPCView{
		Provider:         provider,
		APIKeyConfigured: configured,
	}
}

func maskURLCredentials(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.User = nil
	parsed.Fragment = ""
	query := parsed.Query()
	for key, values := range query {
		for index := range values {
			values[index] = maskedURLValue
		}
		query[key] = values
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func restoreMaskedURL(incoming, existing string) string {
	if incoming == maskURLCredentials(existing) {
		return existing
	}
	incomingURL, incomingErr := url.Parse(incoming)
	existingURL, existingErr := url.Parse(existing)
	if incomingErr != nil || existingErr != nil {
		return incoming
	}
	incomingQuery := incomingURL.Query()
	existingQuery := existingURL.Query()
	if incomingURL.User == nil && existingURL.User != nil {
		incomingURL.User = existingURL.User
	}
	if incomingURL.Fragment == "" && existingURL.Fragment != "" {
		incomingURL.Fragment = existingURL.Fragment
	}
	for key, values := range incomingQuery {
		oldValues := existingQuery[key]
		for index, value := range values {
			if value != maskedURLValue || len(oldValues) == 0 {
				continue
			}
			oldIndex := index
			if oldIndex >= len(oldValues) {
				oldIndex = len(oldValues) - 1
			}
			values[index] = oldValues[oldIndex]
		}
		incomingQuery[key] = values
	}
	incomingURL.RawQuery = incomingQuery.Encode()
	return incomingURL.String()
}

func restoreMaskedURLs(incoming, existing []string) []string {
	restored := append([]string(nil), incoming...)
	for index, candidate := range restored {
		matched := false
		for _, old := range existing {
			if candidate == maskURLCredentials(old) {
				restored[index] = old
				matched = true
				break
			}
		}
		if !matched && index < len(existing) {
			restored[index] = restoreMaskedURL(candidate, existing[index])
		}
	}
	return restored
}
