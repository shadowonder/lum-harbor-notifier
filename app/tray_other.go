//go:build !darwin

package app

func initTray(_ *App) {}

func setNativeHideOnClose(_ bool) {}
