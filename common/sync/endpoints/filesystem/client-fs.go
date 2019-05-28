/*
 * Copyright (c) 2019. Abstrium SAS <team (at) pydio.com>
 * This file is part of Pydio Cells.
 *
 * Pydio Cells is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * Pydio Cells is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with Pydio Cells.  If not, see <http://www.gnu.org/licenses/>.
 *
 * The latest code can be found at <https://pydio.com>.
 */

// Package file system provides endpoints for reading/writing from/to a local folder
package filesystem

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"

	errors2 "github.com/micro/go-micro/errors"
	"github.com/pborman/uuid"
	"github.com/rjeczalik/notify"
	"github.com/spf13/afero"
	"golang.org/x/text/unicode/norm"

	"github.com/pydio/cells/common"
	"github.com/pydio/cells/common/log"
	"github.com/pydio/cells/common/proto/tree"
	"github.com/pydio/cells/common/sync/merger"
	"github.com/pydio/cells/common/sync/model"
	"github.com/pydio/cells/common/sync/proc"
)

const (
	SyncTmpPrefix = ".tmp.write."
)

// TODO MOVE IN A fs_windows FILE TO AVOID RUNTIME OS CHECK
func CanonicalPath(path string) (string, error) {

	if runtime.GOOS == "windows" {
		// Remove any leading slash/backslash
		path = strings.TrimLeft(path, "/\\")
		p, e := filepath.EvalSymlinks(path)
		if e != nil {
			return path, e
		} 
		// Make sure drive letter is lowerCase
		volume := filepath.VolumeName(p)
		if strings.HasSuffix(volume, ":") {
			path = strings.ToLower(volume) + strings.TrimPrefix(p, volume)
		}
	}
	return path, nil

}

type Discarder struct {
	bytes.Buffer
}

func (d *Discarder) Close() error {
	return nil
}

type WrapperWriter struct {
	io.WriteCloser
	tmpPath      string
	targetPath   string
	client       *FSClient
	snapshotPath string
}

func (w *WrapperWriter) Close() error {
	err := w.WriteCloser.Close()
	if err != nil {
		w.client.FS.Remove(w.tmpPath)
		return err
	} else {
		e := w.client.FS.Rename(w.tmpPath, w.targetPath)
		if e == nil && w.client.updateSnapshot != nil {
			ctx := context.Background()
			n, _ := w.client.LoadNode(ctx, w.snapshotPath)
			log.Logger(ctx).Info("[FS] Update Snapshot", n.Zap())
			w.client.updateSnapshot.CreateNode(ctx, n, true)
		}
		return e
	}
}

func (c *FSClient) normalize(path string) string {
	path = strings.TrimLeft(path, string(os.PathSeparator))
	if runtime.GOOS == "darwin" {
		return string(norm.NFC.Bytes([]byte(path)))
	} else if runtime.GOOS == "windows" {
		return strings.Replace(path, string(os.PathSeparator), model.InternalPathSeparator, -1)
	}
	return path
}

func (c *FSClient) denormalize(path string) string {
	// Make sure it starts with a /
	if runtime.GOOS == "darwin" {
		path = fmt.Sprintf("/%v", strings.TrimLeft(path, model.InternalPathSeparator))
		return string(norm.NFD.Bytes([]byte(path)))
	} else if runtime.GOOS == "windows" {
		return strings.Replace(path, model.InternalPathSeparator, string(os.PathSeparator), -1)
	}
	return path
}

// FSClient implementation of an endpoint
// Implements all Sync interfaces (PathSyncTarget, PathSyncSource, DataSyncTarget and DataSyncSource)
// Takes a root folder as main parameter
// Underlying calls to FS are done through Afero.FS virtualization, allowing for mockups and automated testings.
type FSClient struct {
	RootPath       string
	FS             afero.Fs
	updateSnapshot model.PathSyncTarget
	refHashStore   model.PathSyncSource
	options        model.EndpointOptions
	uriPath string
}

func NewFSClient(rootPath string, options model.EndpointOptions) (*FSClient, error) {
	c := &FSClient{
		options: options,
		uriPath: rootPath,
	}
	rootPath = c.denormalize(rootPath)
	rootPath = strings.TrimRight(rootPath, model.InternalPathSeparator)
	var e error
	if c.RootPath, e = CanonicalPath(rootPath); e != nil {
		return nil, e
	}
	if options.BrowseOnly && c.RootPath == "" {
		c.RootPath = "/"
	}
	c.FS = afero.NewBasePathFs(afero.NewOsFs(), c.RootPath)
	if _, e = c.FS.Stat("/"); e != nil {
		return nil, errors.New("Cannot stat root folder " + c.RootPath + "!")
	}
	return c, nil
}

func (c *FSClient) SetUpdateSnapshot(target model.PathSyncTarget) {
	c.updateSnapshot = target
}

func (c *FSClient) PatchUpdateSnapshot(ctx context.Context, patch interface{}) {
	// Reapply event-based patch to snapshot
	if c.updateSnapshot == nil {
		return
	}
	p, ok := patch.(merger.Patch)
	if !ok {
		return
	}
	newPatch := merger.ClonePatch(c, c.updateSnapshot, p)
	newPatch.Filter(ctx)
	pr := proc.NewProcessor(ctx)
	pr.Silent = true
	pr.Process(newPatch)
}

func (c *FSClient) SetRefHashStore(source model.PathSyncSource) {
	c.refHashStore = source
}

func (c *FSClient) GetEndpointInfo() model.EndpointInfo {

	return model.EndpointInfo{
		URI:                   "fs://" + c.uriPath,
		RequiresFoldersRescan: true,
		RequiresNormalization: runtime.GOOS == "darwin",
		//		Ignores:               []string{common.PYDIO_SYNC_HIDDEN_FILE_META},
	}

}

// LoadNode is the Read in CRUD.
// leaf bools are used to avoid doing an FS.stat if we already know a node to be
// a leaf.  NOTE : is it useful?  Examine later.
func (c *FSClient) LoadNode(ctx context.Context, path string, leaf ...bool) (node *tree.Node, err error) {
	return c.loadNode(ctx, path, nil)
}

func (c *FSClient) Walk(walknFc model.WalkNodesFunc, root string, recursive bool) (err error) {
	wrappingFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			walknFc("", nil, err)
			return nil
		}
		if len(path) == 0 || path == "/" || c.normalize(path) == strings.TrimLeft(root, "/") || strings.HasPrefix(filepath.Base(path), SyncTmpPrefix) {
			return nil
		}

		path = c.normalize(path)
		if node, e := c.loadNode(context.Background(), path, info); e != nil {
			walknFc("", nil, e)
		} else {
			walknFc(path, node, nil)
		}

		return nil
	}
	if !recursive {
		infos, er := afero.ReadDir(c.FS, root)
		if er != nil {
			return er
		}
		for _, i := range infos {
			wrappingFunc(path.Join(root, i.Name()), i, nil)
		}
		return nil
	} else {
		return afero.Walk(c.FS, root, wrappingFunc)
	}
}

// Watches for all fs events on an input path.
func (c *FSClient) Watch(recursivePath string) (*model.WatchObject, error) {

	eventChan := make(chan model.EventInfo)
	errorChan := make(chan error)
	doneChan := make(chan bool)
	// Make the channel buffered to ensure no event is dropped. Notify will drop
	// an event if the receiver is not able to keep up the sending pace.
	in, out := PipeChan(1000)

	var fsEvents []notify.Event
	fsEvents = append(fsEvents, EventTypeAll...)

	recursivePath = c.denormalize(recursivePath)
	// Check if FS is in-memory
	_, ok := (c.FS).(*afero.MemMapFs)
	if ok {
		return &model.WatchObject{
			EventInfoChan: eventChan,
			ErrorChan:     errorChan,
			DoneChan:      doneChan,
		}, nil
	}

	if e := notify.Watch(filepath.Join(c.RootPath, recursivePath)+"...", in, fsEvents...); e != nil {
		return nil, e
	}

	// wait for doneChan to close the watcher, eventChan and errorChan
	go func() {
		<-doneChan

		notify.Stop(in)
		close(eventChan)
		close(errorChan)
		close(in)
	}()

	// Get fsnotify notifications for events and errors, and sent them
	// using eventChan and errorChan
	go func() {
		writes := make(map[string]*FSEventDebouncer)
		writesMux := &sync.Mutex{}
		for event := range out {

			if model.IsIgnoredFile(event.Path()) || strings.HasPrefix(filepath.Base(event.Path()), SyncTmpPrefix) {
				continue
			}
			eventInfo, eventError := notifyEventToEventInfo(c, event)
			if eventError != nil {
				errorChan <- eventError
			} else if eventInfo.Path != "" {

				if !eventInfo.Folder {

					var d *FSEventDebouncer
					writesMux.Lock()
					d, o := writes[event.Path()]
					if !o {
						p := event.Path()
						d = NewFSEventDebouncer(eventChan, errorChan, c, func() {
							writesMux.Lock()
							delete(writes, p)
							writesMux.Unlock()
						})
						writes[event.Path()] = d
					}
					writesMux.Unlock()
					d.Input <- eventInfo

				} else {

					eventChan <- eventInfo

				}

			}

		}
	}()

	return &model.WatchObject{
		EventInfoChan: eventChan,
		ErrorChan:     errorChan,
		DoneChan:      doneChan,
	}, nil
}

func (c *FSClient) CreateNode(ctx context.Context, node *tree.Node, updateIfExists bool) (err error) {
	if node.IsLeaf() {
		return errors.New("This is a DataSyncTarget, use PutNode for leafs instead of CreateNode")
	}
	fPath := c.denormalize(node.Path)
	_, e := c.FS.Stat(fPath)
	if os.IsNotExist(e) {
		err = c.FS.MkdirAll(fPath, 0777)
		if node.Uuid != "" && !c.options.BrowseOnly {
			afero.WriteFile(c.FS, filepath.Join(fPath, common.PYDIO_SYNC_HIDDEN_FILE_META), []byte(node.Uuid), 0777)
		}
		if c.updateSnapshot != nil {
			log.Logger(ctx).Info("[FS] Update Snapshot - Create", node.ZapPath())
			c.updateSnapshot.CreateNode(ctx, node, updateIfExists)
		}
	}
	return err
}

func (c *FSClient) UpdateNode(ctx context.Context, node *tree.Node) (err error) {
	return c.CreateNode(ctx, node, true)
}

func (c *FSClient) DeleteNode(ctx context.Context, path string) (err error) {
	_, e := c.FS.Stat(c.denormalize(path))
	if !os.IsNotExist(e) {
		err = c.FS.RemoveAll(c.denormalize(path))
	}
	if err == nil && c.updateSnapshot != nil {
		log.Logger(ctx).Info("[FS] Update Snapshot - Delete " + path)
		c.updateSnapshot.DeleteNode(ctx, path)
	}
	return err
}

// Move file or folder around.
func (c *FSClient) MoveNode(ctx context.Context, oldPath string, newPath string) (err error) {

	oldInitial := oldPath
	newInitial := newPath

	oldPath = c.denormalize(oldPath)
	newPath = c.denormalize(newPath)

	stat, e := c.FS.Stat(oldPath)
	if !os.IsNotExist(e) {
		if stat.IsDir() && reflect.TypeOf(c.FS) == reflect.TypeOf(afero.NewMemMapFs()) {
			c.moveRecursively(oldPath, newPath)
		} else {
			err = c.FS.Rename(oldPath, newPath)
		}
	}
	if err == nil && c.updateSnapshot != nil {
		log.Logger(ctx).Debug("[FS] Update Snapshot - Move from " + oldPath + " to " + newPath)
		c.updateSnapshot.MoveNode(ctx, oldInitial, newInitial)
	}
	return err

}

func (c *FSClient) ComputeChecksum(node *tree.Node) error {
	return fmt.Errorf("not.implemented")
}

func (c *FSClient) ExistingFolders(ctx context.Context) (map[string][]*tree.Node, error) {
	data := make(map[string][]*tree.Node)
	final := make(map[string][]*tree.Node)
	err := c.Walk(func(path string, node *tree.Node, err error) {
		if err != nil || node == nil {
			return
		}
		if node.IsLeaf() {
			return
		}
		if s, ok := data[node.Uuid]; ok {
			s = append(s, node)
			final[node.Uuid] = s
		} else {
			data[node.Uuid] = make([]*tree.Node, 1)
			data[node.Uuid] = append(data[node.Uuid], node)
		}
	}, "/", true)
	return final, err
}

func (c *FSClient) UpdateFolderUuid(ctx context.Context, node *tree.Node) (*tree.Node, error) {
	p := c.denormalize(node.Path)
	var err error
	pFile := filepath.Join(p, common.PYDIO_SYNC_HIDDEN_FILE_META)
	if err = c.FS.Remove(pFile); err == nil {
		log.Logger(ctx).Info("Refreshing folder Uuid for", node.ZapPath())
		err = afero.WriteFile(c.FS, pFile, []byte(node.Uuid), 0666)
	}
	return node, err
}

func (c *FSClient) GetWriterOn(path string, targetSize int64) (out io.WriteCloser, writeDone chan bool, writeErr chan error, err error) {

	// Ignore .pydio except for root folder .pydio
	if filepath.Base(path) == common.PYDIO_SYNC_HIDDEN_FILE_META && strings.Trim(path, "/") != common.PYDIO_SYNC_HIDDEN_FILE_META {
		w := &Discarder{}
		return w, writeDone, writeErr, nil
	}
	snapshotPath := path
	path = c.denormalize(path)
	tmpPath := filepath.Join(filepath.Dir(path), SyncTmpPrefix+filepath.Base(path))
	file, openErr := c.FS.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0666)
	if openErr != nil {
		return nil, writeDone, writeErr, openErr
	}
	wrapper := &WrapperWriter{
		WriteCloser:  file,
		client:       c,
		tmpPath:      tmpPath,
		targetPath:   path,
		snapshotPath: snapshotPath,
	}
	return wrapper, writeDone, writeErr, nil

}

func (c *FSClient) GetReaderOn(path string) (out io.ReadCloser, err error) {

	return c.FS.Open(c.denormalize(path))

}

// Internal function expects already denormalized form
func (c *FSClient) moveRecursively(oldPath string, newPath string) (err error) {

	// Some fs require moving resources recursively
	moves := make(map[int]string)
	indexes := make([]int, 0)
	i := 0
	afero.Walk(c.FS, oldPath, func(wPath string, info os.FileInfo, err error) error {
		//newWPath := newPath + strings.TrimPrefix(wPath, oldPath)
		indexes = append(indexes, i)
		moves[i] = wPath
		i++
		return nil
	})
	total := len(indexes)
	for key := range indexes {
		//c.FS.Rename(moveK, moveV)
		key = total - key
		wPath := moves[key]
		if len(wPath) == 0 {
			continue
		}
		msg := fmt.Sprintf("Moving %v to %v", wPath, newPath+strings.TrimPrefix(wPath, oldPath))
		log.Logger(context.Background()).Debug(msg)
		c.FS.Rename(wPath, newPath+strings.TrimPrefix(wPath, oldPath))
	}
	c.FS.Rename(oldPath, newPath)
	//rename(oldPath,)
	return nil

}

// Expects already denormalized form
func (c *FSClient) getNodeIdentifier(path string, leaf bool) (uid string, e error) {
	if leaf {
		return c.getFileHash(path)
	} else {
		return c.readOrCreateFolderId(path)
	}
}

// Expects already denormalized form
func (c *FSClient) readOrCreateFolderId(path string) (uid string, e error) {

	if c.options.BrowseOnly {
		return uuid.New(), nil
	}
	uidFile, uidErr := c.FS.OpenFile(filepath.Join(path, common.PYDIO_SYNC_HIDDEN_FILE_META), os.O_RDONLY, 0777)
	if uidErr != nil && os.IsNotExist(uidErr) {
		uid = uuid.New()
		we := afero.WriteFile(c.FS, filepath.Join(path, common.PYDIO_SYNC_HIDDEN_FILE_META), []byte(uid), 0666)
		if we != nil {
			return "", we
		}
	} else {
		uidFile.Close()
		content, re := afero.ReadFile(c.FS, filepath.Join(path, common.PYDIO_SYNC_HIDDEN_FILE_META))
		if re != nil {
			return "", re
		}
		uid = fmt.Sprintf("%s", content)
	}
	return uid, nil

}

// Expects already denormalized form
func (c *FSClient) getFileHash(path string) (hash string, e error) {

	f, err := c.FS.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// loadNode takes an optional os.FileInfo if we are already walking folders (no need for a second stat call)
func (c *FSClient) loadNode(ctx context.Context, path string, stat os.FileInfo) (node *tree.Node, err error) {

	dnPath := c.denormalize(path)
	if stat == nil {
		if stat, err = c.FS.Stat(dnPath); err != nil {
			if os.IsNotExist(err) {
				return nil, errors2.NotFound("not.found", path, err)
			}
			return nil, err
		}
	}

	if stat.IsDir() {
		if id, err := c.readOrCreateFolderId(dnPath); err != nil {
			return nil, err
		} else {
			node = &tree.Node{
				Path: path,
				Type: tree.NodeType_COLLECTION,
				Uuid: id,
			}
		}
	} else {
		var hash string
		if c.refHashStore != nil {
			refNode, e := c.refHashStore.LoadNode(ctx, path)
			if e == nil && refNode.Size == stat.Size() && refNode.MTime == stat.ModTime().Unix() && refNode.Etag != "" {
				hash = refNode.Etag
			}
		}
		if len(hash) == 0 {
			if hash, err = c.getFileHash(dnPath); err != nil {
				return nil, err
			}
		}
		node = &tree.Node{
			Path: path,
			Type: tree.NodeType_LEAF,
			Etag: hash,
		}
	}
	node.MTime = stat.ModTime().Unix()
	node.Size = stat.Size()
	node.Mode = int32(stat.Mode())
	return node, nil
}