//go:build integration_tests
// +build integration_tests

package loadimagearchive

import "errors"

// -----------------------------------------------------------------------------
// LoadImageArchive Addon - Builder
// -----------------------------------------------------------------------------

type Builder struct {
	images []string
	ociBin string
}

func NewBuilder(ociBin string) *Builder {
	return &Builder{ociBin: ociBin}
}

func (b *Builder) WithImage(image string) (*Builder, error) {
	if len(image) == 0 {
		return nil, errors.New("no image provided")
	}
	b.images = append(b.images, image)
	return b, nil
}

func (b *Builder) Build() *Addon {
	return &Addon{
		images: b.images,
		ociBin: b.ociBin,
		loaded: false,
	}
}
