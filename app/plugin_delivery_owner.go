package app

import (
	"fmt"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
)

// checkSingleDeliveryOwner enforces the V1 rule that at most one enabled
// notification.deliver Instance may exist, at the configuration mutation
// boundary: a Create or Use Again that would add a second enabled external
// delivery owner is rejected before anything is saved. Only the mutated
// declaration is checked, so disabling one owner of a hand-edited or legacy
// config that already has several enabled owners is still allowed.
// Production startup (RegisterProductionNotificationDelivery) keeps rejecting
// any persisted config with multiple enabled owners as defense in depth.
func checkSingleDeliveryOwner(candidate []config.PluginInstanceConfig, targetIndex int) error {
	target := candidate[targetIndex]
	if target.Disabled || target.CapabilityID != string(plugin.CapabilityNotificationDeliver) {
		return nil
	}
	for index, declaration := range candidate {
		if index == targetIndex || declaration.Disabled || declaration.CapabilityID != target.CapabilityID {
			continue
		}
		return fmt.Errorf("only one notification delivery instance can be in use; stop using %s first", declaration.ID)
	}
	return nil
}

// checkSingleAIProviderOwner enforces the V1 rule that at most one enabled
// ai.provider Instance may exist, at the configuration mutation boundary:
// a Create or Use Again that would add a second enabled AI provider is
// rejected before anything is saved. Only the mutated declaration is checked,
// so disabling one owner of a hand-edited or legacy config that already has
// several enabled owners is still allowed.
// Production startup composition keeps detecting startup conflicts as defense in depth.
func checkSingleAIProviderOwner(candidate []config.PluginInstanceConfig, targetIndex int) error {
	target := candidate[targetIndex]
	if target.Disabled || target.CapabilityID != string(plugin.CapabilityAIProvider) {
		return nil
	}
	for index, declaration := range candidate {
		if index == targetIndex || declaration.Disabled || declaration.CapabilityID != target.CapabilityID {
			continue
		}
		return fmt.Errorf("only one AI provider instance can be in use; stop using %s first", declaration.ID)
	}
	return nil
}
