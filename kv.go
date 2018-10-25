package vaultkv

type KV struct {
	client *Client
	//Map from mount name to [true if version 2. False otherwise]
	mounts map[string]kvMount
}

type kvMount interface {
	Get(path string, output interface{}, opts *KVGetOpts) (meta KVVersion, err error)
	Set(path string, values map[string]string, opts *KVSetOpts) (meta KVVersion, err error)
	Delete(path string, versions []uint) (err error)
	Undelete(path string, versions []uint) (err error)
	Destroy(path string, versions []uint) (err error)
	DestroyAll(path string) (err error)
	Versions(path string) (ret []KVVersion, err error)
	MountVersion() (version uint)
}

/*====================
        KV V1
====================*/
type kvv1Mount struct {
	client *Client
}

func (k kvv1Mount) Get(path string, output interface{}, opts *KVGetOpts) (meta KVVersion, err error) {
	if opts.Version > 1 {
		err = &ErrNotFound{"No versions greater than one in KV v1 backend"}
		return
	}

	err = k.client.Get(path, output)
	if err != nil {
		meta.Version = 1
	}
	return
}

func (k kvv1Mount) Set(path string, values map[string]string, opts *KVSetOpts) (meta KVVersion, err error) {
	err = k.client.Set(path, values)
	if err != nil {
		meta.Version = 1
	}
	return
}

func (k kvv1Mount) Delete(path string, versions []uint) (err error) {
	return k.client.Delete(path)
}

func (k kvv1Mount) Undelete(path string, versions []uint) (err error) {
	return &ErrKVUnsupported{"Cannot undelete secret in KV v1 backend"}
}

func (k kvv1Mount) Destroy(path string, versions []uint) (err error) {
	return k.client.Delete(path)
}

func (k kvv1Mount) DestroyAll(path string) (err error) {
	return k.client.Delete(path)
}

func (k kvv1Mount) Versions(path string) (ret []KVVersion, err error) {
	err = k.client.Get(path, nil)
	if err != nil {
		return nil, err
	}
	ret = []KVVersion{{Version: 1}}
	return
}

func (k kvv1Mount) MountVersion() (version uint) {
	return 1
}

/*====================
        KV V2
====================*/
type kvv2Mount struct {
	client *Client
}

func (k kvv2Mount) Get(path string, output interface{}, opts *KVGetOpts) (meta KVVersion, err error) {
	var o *V2GetOpts
	if opts != nil {
		o = &V2GetOpts{
			Version: opts.Version,
		}
	}

	var m V2Version
	m, err = k.client.V2Get(path, output, o)
	if err != nil {
		meta.Deleted = m.DeletedAt != nil
		meta.Destroyed = m.Destroyed
		meta.Version = m.Version
	}
	return
}

func (k kvv2Mount) Set(path string, values map[string]string, opts *KVSetOpts) (meta KVVersion, err error) {
	var m V2Version
	m, err = k.client.V2Set(path, values, nil)
	if err != nil {
		meta.Version = m.Version
	}
	return
}

func (k kvv2Mount) Delete(path string, versions []uint) (err error) {
	return k.client.V2Delete(path, &V2DeleteOpts{Versions: versions})
}

func (k kvv2Mount) Undelete(path string, versions []uint) (err error) {
	return k.client.V2Undelete(path, versions)
}

func (k kvv2Mount) Destroy(path string, versions []uint) (err error) {
	return k.client.V2Destroy(path, versions)
}

func (k kvv2Mount) DestroyAll(path string) (err error) {
	return k.client.V2DestroyMetadata(path)
}

func (k kvv2Mount) Versions(path string) (ret []KVVersion, err error) {
	var meta V2Metadata
	meta, err = k.client.V2GetMetadata(path)
	if err != nil {
		return nil, err
	}

	ret = make([]KVVersion, len(meta.Versions))
	for i := range meta.Versions {
		ret[i].Deleted = meta.Versions[i].DeletedAt != nil
		ret[i].Destroyed = meta.Versions[i].Destroyed
		ret[i].Version = meta.Versions[i].Version
	}
	return
}

func (k kvv2Mount) MountVersion() (version uint) {
	return 2
}

/*==========================
       KV Abstraction
==========================*/
func (v *Client) NewKV() *KV {
	return &KV{client: v, mounts: map[string]kvMount{}}
}

func (k *KV) mountForPath(path string) (ret kvMount, err error) {
	mount, _ := SplitMount(path)
	ret, found := k.mounts[mount]
	if found {
		return
	}

	isV2, err := k.client.IsKVv2Mount(mount)
	if err != nil {
		return
	}

	ret = kvv1Mount{k.client}
	if isV2 {
		ret = kvv2Mount{k.client}
	}

	return
}

type KVGetOpts struct {
	// Version is the version of the resource to retrieve. Setting this to zero (or
	// not setting it at all) will retrieve the latest version
	Version uint
}

type KVVersion struct {
	Version   uint
	Deleted   bool
	Destroyed bool
}

func (k *KV) Get(path string, output interface{}, opts *KVGetOpts) (meta KVVersion, err error) {
	mount, err := k.mountForPath(path)
	if err != nil {
		return
	}

	return mount.Get(path, output, opts)
}

//KVSetOpts are the options for a set call to the KV.Set() call. Currently there
// are none, but it exists in case the API adds support in the future for things
// that we can put here.
type KVSetOpts struct{}

func (k *KV) Set(path string, values map[string]string, opts *KVSetOpts) (meta KVVersion, err error) {
	mount, err := k.mountForPath(path)
	if err != nil {
		return
	}

	return mount.Set(path, values, opts)
}

func (k *KV) Delete(path string, versions []uint) (err error) {
	mount, err := k.mountForPath(path)
	if err != nil {
		return
	}

	return mount.Delete(path, versions)
}

func (k *KV) Undelete(path string, versions []uint) (err error) {
	mount, err := k.mountForPath(path)
	if err != nil {
		return
	}

	return mount.Undelete(path, versions)
}

func (k *KV) Destroy(path string, versions []uint) (err error) {
	mount, err := k.mountForPath(path)
	if err != nil {
		return
	}

	return mount.Destroy(path, versions)
}

func (k *KV) DestroyAll(path string) (err error) {
	mount, err := k.mountForPath(path)
	if err != nil {
		return
	}

	return mount.DestroyAll(path)
}

func (k *KV) Versions(path string) (ret []KVVersion, err error) {
	mount, err := k.mountForPath(path)
	if err != nil {
		return
	}

	return mount.Versions(path)
}

func (k *KV) MountVersion(mount string) (version uint, err error) {
	m, err := k.mountForPath(mount)
	if err != nil {
		return
	}

	return m.MountVersion(), nil
}
