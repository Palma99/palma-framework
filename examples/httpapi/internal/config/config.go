package config

// Config is supplied by the composition root. Environment loading comes later.
type Config struct {
	HTTPAddress  string
	SeedUserName string
}
