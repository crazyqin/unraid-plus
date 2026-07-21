package ssh

import (
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPClient wraps a *sftp.Client so callers don't need to import that package
// directly. The lifecycle of the underlying ssh.Session is owned here.
type SFTPClient struct {
	sc  *sftp.Client
	ss  *ssh.Session
}

func newSFTPClient(r io.Reader, w io.WriteCloser, sess *ssh.Session) (*SFTPClient, error) {
	sc, err := sftp.NewClientPipe(r, w)
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("sftp handshake: %w", err)
	}
	return &SFTPClient{sc: sc, ss: sess}, nil
}

// Close releases both the sftp client and the underlying ssh session.
func (c *SFTPClient) Close() error {
	err := c.sc.Close()
	if e := c.ss.Close(); e != nil && err == nil {
		err = e
	}
	return err
}

// Entry is the API-friendly view of an SFTP file/dir.
type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"sizeBytes"`
	ModTime  int64  `json:"modTime"`
	Mode     string `json:"mode"`
	Owner    string `json:"owner"`
	Group    string `json:"group"`
}

// List reads a directory and returns entries sorted by (dir first, then name).
func (c *SFTPClient) List(p string) ([]Entry, error) {
	infos, err := c.sc.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(infos))
	for _, info := range infos {
		out = append(out, Entry{
			Name:    info.Name(),
			Path:    path.Join(p, info.Name()),
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			Mode:    info.Mode().String(),
			Owner:   "-", // sftp.Stat_t owner would need c.sc.Stat; left as — for v0.x
			Group:   "-",
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Stat returns info about a single path.
func (c *SFTPClient) Stat(p string) (Entry, error) {
	info, err := c.sc.Stat(p)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		Name:    path.Base(p),
		Path:    p,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime().Unix(),
		Mode:    info.Mode().String(),
	}, nil
}

// Open opens a file for reading. Caller must Close the returned io.ReadCloser.
func (c *SFTPClient) Open(p string) (io.ReadCloser, error) {
	return c.sc.Open(p)
}

// Create opens (or truncates) a file for writing.
func (c *SFTPClient) Create(p string) (io.WriteCloser, error) {
	return c.sc.Create(p)
}

// Mkdir creates a directory (including parents).
func (c *SFTPClient) Mkdir(p string) error {
	return c.sc.MkdirAll(p)
}

// Remove removes a file or empty directory. For recursive removal use RemoveAll.
func (c *SFTPClient) Remove(p string) error {
	return c.sc.Remove(p)
}

// RemoveAll recursively removes a path (best-effort).
func (c *SFTPClient) RemoveAll(p string) error {
	info, err := c.sc.Stat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return c.sc.Remove(p)
	}
	entries, err := c.sc.ReadDir(p)
	if err != nil {
		return err
	}
	for _, e := range entries {
		full := path.Join(p, e.Name())
		if e.IsDir() {
			if err := c.RemoveAll(full); err != nil {
				return err
			}
		} else {
			if err := c.sc.Remove(full); err != nil {
				return err
			}
		}
	}
	return c.sc.RemoveDirectory(p)
}

// Move is rename/move.
func (c *SFTPClient) Move(src, dst string) error {
	return c.sc.Rename(src, dst)
}

// Touch ensures a file exists and updates its mtime to now.
func (c *SFTPClient) Touch(p string) error {
	if _, err := c.sc.Stat(p); os.IsNotExist(err) {
		f, err := c.sc.Create(p)
		if err != nil {
			return err
		}
		return f.Close()
	} else if err != nil {
		return err
	}
	return c.sc.Chtimes(p, time.Now(), time.Now())
}
