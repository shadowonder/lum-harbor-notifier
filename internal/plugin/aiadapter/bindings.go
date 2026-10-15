package aiadapter

import (
	"fmt"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/plugin"
)

func ProviderRegistration(extension plugin.Extension, invoker plugin.Invoker, instance *plugin.Instance) (ai.ProviderRegistration, error) {
	for _, binding := range extension.Manifest.Bindings {
		if binding.Type == plugin.BindingAIProvider {
			return ai.ProviderRegistration{
				ID: extension.Manifest.ID, Name: extension.Manifest.Name,
				Provider: NewProvider(invoker, extension.Manifest.ID, instance),
			}, nil
		}
	}
	return ai.ProviderRegistration{}, fmt.Errorf("extension %q has no AI provider binding", extension.Manifest.ID)
}

func PreBindings(extension plugin.Extension, invoker plugin.Invoker, instance *plugin.Instance) []ai.PreMiddlewareBinding {
	var result []ai.PreMiddlewareBinding
	for _, binding := range extension.Manifest.Bindings {
		if binding.Type != plugin.BindingAIPre {
			continue
		}
		all, actions := actionsForBinding(binding)
		result = append(result, ai.PreMiddlewareBinding{
			ID: binding.ID, Name: extension.Manifest.Name, AllActions: all, Actions: actions,
			Middleware: NewPreMiddleware(invoker, extension.Manifest.ID, instance),
		})
	}
	return result
}

func PostBindings(extension plugin.Extension, invoker plugin.Invoker, instance *plugin.Instance) []ai.PostMiddlewareBinding {
	var result []ai.PostMiddlewareBinding
	for _, binding := range extension.Manifest.Bindings {
		if binding.Type != plugin.BindingAIPost {
			continue
		}
		all, actions := actionsForBinding(binding)
		result = append(result, ai.PostMiddlewareBinding{
			ID: binding.ID, Name: extension.Manifest.Name, AllActions: all, Actions: actions,
			Middleware: NewPostMiddleware(invoker, extension.Manifest.ID, instance),
		})
	}
	return result
}

func actionsForBinding(binding plugin.BindingDeclaration) (bool, []ai.Action) {
	if len(binding.Actions) == 1 && binding.Actions[0] == "*" {
		return true, nil
	}
	actions := make([]ai.Action, len(binding.Actions))
	for index, action := range binding.Actions {
		actions[index] = ai.Action(action)
	}
	return false, actions
}
