package port

import "asana_extractor/internal/config/domain"

// Loader builds a Config from some external source (env, flags, file, ...).
type Loader interface {
	Load() (*domain.Config, error)
}
