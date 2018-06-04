package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/upyun/go-sdk/upyun"
)

func (sess *Session) GetFromFile(filename, localPath string, workers int) {
	fileChan := make(chan string, 2*workers)
	localPath, _ = filepath.Abs(localPath)
	var wg sync.WaitGroup

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for filename := range fileChan {
				src := path.Join("/", filename)
				dst := filepath.Join(localPath, filename)
				if fInfo, _ := os.Stat(dst); fInfo != nil {
					Print("get %s EXIST", src)
					continue
				}
				err := os.MkdirAll(filepath.Dir(dst), 0755)
				if err != nil {
					PrintError("get %s: %v", src, err)
					continue
				}

				_, err = sess.updriver.Get(&upyun.GetObjectConfig{
					Path:      src,
					LocalPath: dst,
				})
				if err != nil {
					PrintError("get %s: %v", src, err)
				} else {
					Print("get %s %s OK", src, dst)
				}
			}
		}()
	}

	fd, err := os.Open(filename)
	if err != nil {
		PrintErrorAndExit("open %s: %v", filename, err)
	}
	defer fd.Close()

	scanner := bufio.NewScanner(fd)
	for scanner.Scan() {
		line := scanner.Text()
		fileChan <- strings.Split(line, "\t")[0]
	}
	close(fileChan)

	wg.Wait()
}

func (sess *Session) RangeGet(localPath string, start, end int64, workers int) {
	objChan := make(chan *upyun.FileInfo, 2*workers)
	localPath, _ = filepath.Abs(localPath)

	if err := initDB(); err != nil {
		PrintErrorAndExit("sync: init database: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for fInfo := range objChan {
				dst := filepath.Join(localPath, fInfo.Name)
				key := []byte(fmt.Sprintf("rget:%s:%s", path.Join("/", sess.Bucket, fInfo.Name), dst))
				value, err := db.Get(key, nil)
				err = os.MkdirAll(filepath.Dir(dst), 0755)
				if err != nil {
					PrintErrorAndExit("mkdir: %v", err)
				}
				if len(value) == 0 || string(value) != fInfo.MD5 {
					_, err := sess.updriver.Get(&upyun.GetObjectConfig{
						Path:      fInfo.Name,
						LocalPath: dst,
					})
					if err != nil {
						PrintError("rget %s: %v", fInfo.Name, err)
					} else {
						Print("rget %s %s %s OK", fInfo.Name, dst, fInfo.Time)
					}

					err = db.Put(key, []byte(fInfo.MD5), nil)
					if err != nil {
						PrintError("set leveldb: %s %v", fInfo.Name, err)
					}
				} else {
					Print("rget %s %s %s EXIST", fInfo.Name, fInfo.Time, dst)
				}
			}
		}()
	}

	err := sess.updriver.RangeList(&upyun.RangeObjectsConfig{
		StartTimestamp: start,
		EndTimestamp:   end,
		ObjectsChan:    objChan,
	})
	if err != nil {
		PrintErrorAndExit("range list: %v", err)
	}

	wg.Wait()
}