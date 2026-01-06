package dev

import "github.com/digitalocean/go-libvirt"

// SpecSource defines function used to define dev environment elements.
type SpecSource func(l *libvirt.Libvirt) error
