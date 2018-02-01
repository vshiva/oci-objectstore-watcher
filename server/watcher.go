package server

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/oracle/oci-go-sdk/objectstorage"
)

const (
	add = "NEW"
	del = "DELETE"
	upd = "UPDATE"
)

type ObjectWatcher struct {
	Namespace    string
	Buckets      []string
	WebhookURI   string
	PollInterval time.Duration
	quit         chan bool
}

type Payload struct {
	Namespace   string `json:"namespace"`
	Bucket      string `json:"bucket"`
	ObjectName  string `json:"objectName"`
	ContentHash string `json:"contentHash"`
	Type        string `json:"type"`
}

func (o *ObjectWatcher) Watch(client objectstorage.ObjectStorageClient) {

	o.quit = make(chan bool)
	for _, b := range o.Buckets {
		bucket := b
		go func() {
			cache, err := o.loadCache(bucket)
			if err != nil {
				return
			}
			for {
				ticker := time.NewTicker(o.PollInterval)
				select {
				case <-ticker.C:
					o.updateCache(cache, client, bucket)
					o.saveCache(cache, bucket)
				case <-o.quit: // This channel will be closed on shutdown, so all go routines will get this message
					ticker.Stop()
					return
				}
			}
		}()
	}
}

// Shutdown shuts down the watcher gracefully
func (o *ObjectWatcher) Shutdown() {
	close(o.quit)
}

func (o *ObjectWatcher) loadCache(bucket string) (map[string]string, error) {
	file, err := os.Open(bucket)
	cache := make(map[string]string)
	if err != nil {
		return cache, nil
	}
	if err := gob.NewDecoder(file).Decode(&cache); err != nil {
		return nil, err
	}
	return cache, nil
}

func (o *ObjectWatcher) saveCache(cache map[string]string, bucket string) {
	file, err := os.Create(bucket)
	if err != nil {
		fmt.Printf("Failed to save snapshot of cache to file due to %v\n", err)
		return
	}
	if err = gob.NewEncoder(file).Encode(cache); err != nil {
		fmt.Printf("Failed to save snapshot of cache to file due to %v\n", err)
	}
}

func (o *ObjectWatcher) updateCache(cache map[string]string, client objectstorage.ObjectStorageClient, bucket string) {

	newList, err := o.list(client, bucket)
	if err != nil {
		fmt.Printf("Failed to fetch object list: %v\n", err)
		return
	}

	for name, md5 := range cache {

		newMd5, ok := newList[name]
		if !ok {
			delete(cache, name)
			o.callHook(del, bucket, name, md5)
		} else if newMd5 != md5 {
			cache[name] = newMd5
			o.callHook(upd, bucket, name, newMd5)
		}
		delete(newList, name)
	}

	for name, md5 := range newList {
		cache[name] = md5
		o.callHook(add, bucket, name, md5)
	}
}

func (o *ObjectWatcher) list(client objectstorage.ObjectStorageClient, bucket string) (map[string]string, error) {

	limit := 1000
	startWith := ""
	objects := make(map[string]string)
	for {
		response, err := client.ListObjects(context.Background(), objectstorage.ListObjectsRequest{
			BucketName:    &bucket,
			Fields:        "name,md5",
			Limit:         &limit,
			NamespaceName: &o.Namespace,
			Start:         &startWith,
		})

		if err != nil {
			return nil, err
		}

		for _, object := range response.ListObjects.Objects {
			objects[*object.Name] = *object.Md5
		}

		if response.NextStartWith == nil || *response.NextStartWith == "" {
			return objects, nil
		}
		startWith = *response.NextStartWith
	}
}

func (o *ObjectWatcher) callHook(event string, bucket string, objectName string, md5 string) error {
	payload := Payload{
		Bucket:      bucket,
		Type:        event,
		ContentHash: md5,
		Namespace:   o.Namespace,
		ObjectName:  objectName,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	fmt.Printf("Posting %v to %s\n", string(b), o.WebhookURI)
	_, err = http.DefaultClient.Post(o.WebhookURI, "application/json", bytes.NewBuffer(b))
	return err
}
