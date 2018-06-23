package vaultkv

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (c *Client) IsKVv2Mount(path string) (bool, error) {
	output := struct {
		Type    string `json:"type"`
		Options struct {
			Version string `json:"version"`
		} `json:"options"`
	}{}

	err := c.doRequest(
		"GET",
		fmt.Sprintf("/sys/internal/ui/mounts/%s", strings.TrimLeft(path, "/")),
		nil, &output)
	if err != nil {
		//If we got a 404, either the mount doesn't exist or this version of Vault
		// is too old to possibly have a v2 backend
		if _, is404 := err.(*ErrNotFound); is404 {
			return false, nil
		}
		return false, err
	}

	if output.Type != "kv" {
		return false, nil
	}

	version, err := strconv.ParseUint(output.Options.Version, 10, 64)
	if err != nil {
		return false, nil
	}

	return version == 2, nil
}

func splitMount(path string) (mount string, subpath string) {
	path = strings.TrimLeft(path, "/")
	splits := strings.SplitN(path, "/", 2)
	mount = splits[0]
	if len(splits) > 1 {
		subpath = splits[1]
	}

	subpath = fmt.Sprintf("/%s", strings.TrimLeft(subpath, "/"))
	return
}

//V2Version is information about a version of a secret. The DeletedAt member
// will be nil to signify that a version is not deleted. Take note of the
// difference between "deleted" and "destroyed" - a deletion simply marks a
// secret as deleted, preventing it from being read. A destruction actually
// removes the data from storage irrevocably.
type V2Version struct {
	CreatedAt time.Time
	DeletedAt *time.Time
	Destroyed bool
	Version   uint
}

type v2VersionAPI struct {
	CreatedTime  string `json:"created_time"`
	DeletionTime string `json:"deletion_time"`
	Destroyed    bool   `json:"destroyed"`
	Version      uint   `json:"version"`
}

func (v v2VersionAPI) Parse() V2Version {
	ret := V2Version{
		Destroyed: v.Destroyed,
		Version:   v.Version,
	}

	//Parse those times
	ret.CreatedAt, _ = time.Parse(time.RFC3339Nano, v.CreatedTime)
	tmpDeletion, err := time.Parse(time.RFC3339Nano, v.DeletionTime)
	if err == nil {
		ret.DeletedAt = &tmpDeletion
	}

	return ret
}

type V2GetOpts struct {
	// Version is the version of the resource to retrieve. Setting this to zero (or
	// not setting it at all) will retrieve the latest version
	Version uint
}

func (v *Client) V2Get(path string, output interface{}, opts *V2GetOpts) (meta V2Version, err error) {
	unmarshalInto := struct {
		Metadata v2VersionAPI `json:"metadata"`
		Data     interface{}  `json:"data"`
	}{
		Metadata: v2VersionAPI{},
	}

	if output != nil {
		unmarshalInto.Data = &output
	}

	query := url.Values{}
	if opts != nil {
		query.Add("version", strconv.FormatUint(uint64(opts.Version), 10))
	}

	mount, subpath := splitMount(path)
	path = fmt.Sprintf("%s/data%s", mount, subpath)
	err = v.doRequest("GET", path, query, unmarshalInto)
	if err != nil {
		return
	}

	meta = unmarshalInto.Metadata.Parse()
	return
}

type V2SetOpts struct {
	CAS *uint `json:"cas,omitempty"`
}

//SetCAS sets the check-and-set option for a write. If i is zero, then the value
//will only be written if the key does not exist. If i is non-zero, then the
//value will only be written if the currently existing version matches i. Not
//calling CAS will result in no restriction on writing. If the mount is set up
//for requiring CAS, then setting CAS with this function a valid number will
//result in a failure when attempting to write.
func (s *V2SetOpts) SetCAS(i uint) *V2SetOpts {
	s.CAS = new(uint)
	*s.CAS = i
	return s
}

func (v *Client) V2Set(path string, values map[string]string, opts *V2SetOpts) (meta V2Version, err error) {
	input := struct {
		Options *V2SetOpts        `json:"options,omitempty"`
		Data    map[string]string `json:"data"`
	}{
		Options: opts,
		Data:    values,
	}

	output := struct {
		Data v2VersionAPI `json:"data"`
	}{
		Data: v2VersionAPI{},
	}

	mount, subpath := splitMount(path)
	path = fmt.Sprintf("%s/data%s", mount, subpath)

	err = v.doRequest("PUT", path, &input, &output)
	if err != nil {
		return
	}

	meta = output.Data.Parse()
	return
}

type V2DeleteOpts struct {
	Versions []uint `json:"versions"`
}

func (v *Client) V2Delete(path string, opts *V2DeleteOpts) error {
	method := "DELETE"
	mount, subpath := splitMount(path)
	path = fmt.Sprintf("%s/data%s", mount, subpath)

	if opts != nil && len(opts.Versions) > 0 {
		method = "POST"
		path = fmt.Sprintf("%s/delete%s")
	} else {
		opts = nil
	}

	return v.doRequest(method, path, opts, nil)
}

func (v *Client) V2Undelete(path string, versions []uint) error {
	mount, subpath := splitMount(path)
	path = fmt.Sprintf("%s/undelete%s", mount, subpath)
	return v.doRequest("POST", path, struct {
		Versions []uint `json:"versions"`
	}{
		Versions: versions,
	}, nil)
}

func (v *Client) V2Destroy(path string, versions []uint) error {
	mount, subpath := splitMount(path)
	path = fmt.Sprintf("%s/destroy%s", mount, subpath)
	return v.doRequest("POST", path, struct {
		Versions []uint `json:"versions"`
	}{
		Versions: versions,
	}, nil)
}

func (v *Client) V2DestroyMetadata(path string) error {
	mount, subpath := splitMount(path)
	path = fmt.Sprintf("%s/metadata%s", mount, subpath)
	return v.doRequest("DELETE", path, nil, nil)
}

type V2Metadata struct {
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CurrentVersion uint
	OldestVersion  uint
	MaxVersions    uint
	Versions       []V2Version
}

type v2MetadataAPI struct {
	Data struct {
		CreatedTime    string                  `json:"created_time"`
		CurrentVersion uint                    `json:"current_version"`
		MaxVersions    uint                    `json:"max_versions"`
		OldestVersion  uint                    `json:"oldest_version"`
		UpdatedTime    string                  `json:"updated_time"`
		Versions       map[string]v2VersionAPI `json:"versions"`
	} `json:"data"`
}

func (m v2MetadataAPI) Parse() V2Metadata {
	ret := V2Metadata{
		CurrentVersion: m.Data.CurrentVersion,
		MaxVersions:    m.Data.MaxVersions,
		OldestVersion:  m.Data.OldestVersion,
	}

	ret.CreatedAt, _ = time.Parse(time.RFC3339Nano, m.Data.CreatedTime)
	ret.UpdatedAt, _ = time.Parse(time.RFC3339Nano, m.Data.UpdatedTime)

	for number, metadata := range m.Data.Versions {
		toAdd := metadata.Parse()
		version64, _ := strconv.ParseUint(number, 10, 64)
		toAdd.Version = uint(version64)
		ret.Versions = append(ret.Versions, toAdd)
	}

	sort.Slice(ret.Versions,
		func(i, j int) bool { return ret.Versions[i].Version < ret.Versions[j].Version },
	)

	return ret
}

func (v *Client) V2GetMetadata(path string) (meta V2Metadata, err error) {
	mount, subpath := splitMount(path)
	path = fmt.Sprintf("%s/metadata%s", mount, subpath)
	output := v2MetadataAPI{}
	err = v.doRequest("DELETE", path, nil, &output)
	if err != nil {
		return
	}
	meta = output.Parse()
	return
}
