package readcap

// Fact is one small named fact returned by a PLUGIN-origin read capability.
type Fact struct {
	Name  string
	Value string
}

// PluginQueryOutput is the Result.Output shape for every PLUGIN-origin read
// capability (M15 Slice 2). It is a readcap-owned data shape — small and
// bounded like every CORE output — so a caller never needs to import
// internal/plugin's own ReadQueryResult/ReadFact types to read a Plugin
// capability's result.
type PluginQueryOutput struct {
	Summary string
	Facts   []Fact
}
