package idgen

import (
	"crypto/rand"

	"github.com/ridge/must"
)

type randomGenerator struct{}

// Random is the global ID generator that produces random identifiers.
var Random Generator = randomGenerator{}

func (rs randomGenerator) ID() string {
	id := make([]byte, 16)
	for {
		must.Any(rand.Read(id))
		s := encodeID(id)
		if s[0] >= 'a' && s[0] <= 'z' {
			return s
		}
	}
}