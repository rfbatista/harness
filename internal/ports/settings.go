package ports

// Driving ports of the settings context. *settings.Service satisfies them.

// SettingsEditor reads and writes application settings.
type SettingsEditor interface {
	GetSettings() (map[string]string, error)
	UpdateSettings(values map[string]string) (map[string]string, error)
}

// SettingsReader reads one setting; empty means unset.
type SettingsReader interface {
	Setting(key string) string
}

// Settings is the whole settings context.
type Settings interface {
	SettingsEditor
	SettingsReader
}
