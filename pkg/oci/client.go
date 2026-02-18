// Package oci wraps the shared giantswarm/klaus-oci library and adds
// klausctl-specific helpers such as CLI cache paths, plugin directory
// resolution, and container mount path computation.
//
// All OCI media types, metadata types, annotation constants, and the
// ORAS-based Client come from the shared library. klausctl code should
// import this package rather than klaus-oci directly to avoid import
// alias churn.
package oci

import (
	klausoci "github.com/giantswarm/klaus-oci"
)

// Re-exported types from the shared klaus-oci library.
type (
	Client          = klausoci.Client
	ClientOption    = klausoci.ClientOption
	ArtifactKind    = klausoci.ArtifactKind
	PullResult      = klausoci.PullResult
	PushResult      = klausoci.PushResult
	PluginMeta      = klausoci.PluginMeta
	PersonalityMeta = klausoci.PersonalityMeta
	PersonalitySpec = klausoci.PersonalitySpec
	PluginReference = klausoci.PluginReference
	ToolchainMeta   = klausoci.ToolchainMeta
	CacheEntry      = klausoci.CacheEntry
	ArtifactInfo    = klausoci.ArtifactInfo
)

// Re-exported constructors, options, and helpers.
var (
	NewClient           = klausoci.NewClient
	WithPlainHTTP       = klausoci.WithPlainHTTP
	WithRegistryAuthEnv = klausoci.WithRegistryAuthEnv

	PluginArtifact      = klausoci.PluginArtifact
	PersonalityArtifact = klausoci.PersonalityArtifact

	IsCached       = klausoci.IsCached
	ReadCacheEntry = klausoci.ReadCacheEntry

	ShortName      = klausoci.ShortName
	TruncateDigest = klausoci.TruncateDigest
)

// Re-exported media type and annotation constants.
const (
	MediaTypePluginConfig       = klausoci.MediaTypePluginConfig
	MediaTypePluginContent      = klausoci.MediaTypePluginContent
	MediaTypePersonalityConfig  = klausoci.MediaTypePersonalityConfig
	MediaTypePersonalityContent = klausoci.MediaTypePersonalityContent

	AnnotationKlausType    = klausoci.AnnotationKlausType
	AnnotationKlausName    = klausoci.AnnotationKlausName
	AnnotationKlausVersion = klausoci.AnnotationKlausVersion

	TypePlugin      = klausoci.TypePlugin
	TypePersonality = klausoci.TypePersonality
	TypeToolchain   = klausoci.TypeToolchain
)
