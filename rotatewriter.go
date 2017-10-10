// Package rotatewiter contains additional tool for logging packages - RotateWriter Writer which implemet normal fast smooth rotation
package rotatewriter

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// this file contains realizarion of Writer for logs which contains rotate capability

// RotateWriter is Writer with Rotate function to make correctly rotation of
type RotateWriter struct {
	Filename    string
	NumFiles    int
	dirpath     string
	file        *os.File
	writeMutex  sync.Mutex
	rotateMutex sync.Mutex
	statusMap   sync.Map
}

// NewRotateWriter creates new instance make some checks there
// fname: filename, must contain existing directory  file
// numfiles: 0 if no rotation at all - just reopen file on rotation. e.g. you would like use logrotate
// numfiles: >0 if rotation enabled
func NewRotateWriter(fname string, numfiles int) (rw *RotateWriter, err error) {
	rw = &RotateWriter{Filename: fname, NumFiles: numfiles, file: nil}
	err = rw.initDirPath()
	if nil != err {
		return nil, err
	}
	err = rw.openWriteFile()
	if nil != err {
		return nil, err
	}
	if 0 > numfiles {
		return nil, fmt.Errorf("numfiles must be 0 or more")
	}
	return rw, nil
}

// initDirPath gets dir path from filename and init them
func (rw *RotateWriter) initDirPath() error {
	if rw.Filename == "" {
		return fmt.Errorf("Wrong log path")
	}
	rw.dirpath = filepath.Dir(rw.Filename)
	fileinfo, err := os.Stat(rw.dirpath)
	if err != nil {
		return err
	}
	if !fileinfo.IsDir() {
		return fmt.Errorf("Path to log file %s is not directory", rw.dirpath)
	}
	return nil
}

// openWriteFile warning - is not safe - use Lock unlock while work
func (rw *RotateWriter) openWriteFile() error {
	file, err := rw.openWriteFileInt()
	if err != nil {
		rw.file = nil
		return err
	}
	rw.file = file
	return nil
}

func (rw *RotateWriter) openWriteFileInt() (file *os.File, err error) {
	fileinfo, err := os.Stat(rw.Filename)
	newFile := false
	if err != nil {
		if os.IsNotExist(err) {
			newFile = true
			err = nil
		} else {
			return nil, err
		}
	}
	if fileinfo == nil {
		newFile = true
	} else {
		if fileinfo.IsDir() {
			return nil, fmt.Errorf("File %s is a directory", rw.Filename)
		}
	}
	if newFile {
		file, err = os.OpenFile(rw.Filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	} else {
		file, err = os.OpenFile(rw.Filename, os.O_APPEND|os.O_WRONLY, 0644)
	}
	return file, err
}

// CloseWriteFile use to close writer if you need
func (rw *RotateWriter) CloseWriteFile() error {
	rw.writeMutex.Lock()
	defer rw.writeMutex.Unlock()
	if rw.file == nil {
		return nil
	}
	err := rw.file.Close()
	rw.file = nil
	return err
}

// Write implements io.Writer
func (rw *RotateWriter) Write(p []byte) (n int, err error) {
	rw.writeMutex.Lock()
	defer rw.writeMutex.Unlock()
	if rw.file == nil {
		return 0, fmt.Errorf("Error: no file was opened for work with")
	}
	n, err = rw.file.Write(p)
	return n, err
}

// RotationInProgress detects rotation is running now
func (rw *RotateWriter) RotationInProgress() bool {
	_, ok := rw.statusMap.Load("rotation")
	return ok
}

// Rotate rotates file
func (rw *RotateWriter) Rotate(ready func()) error {
	if _, ok := rw.statusMap.Load("rotation"); ok {
		// rotation in progress - just prevent all fuckups
		return nil
	}
	rw.rotateMutex.Lock()
	defer rw.rotateMutex.Unlock()
	defer func() {
		if nil == ready {
			return
		}
		ready()
	}()
	rw.statusMap.Store("rotation", true)
	defer rw.statusMap.Delete("rotation")
	files, err := ioutil.ReadDir(rw.dirpath)
	if err != nil {
		return err
	}
	_, fname := filepath.Split(rw.Filename)
	sl := make([]int, 0, rw.NumFiles)
	if rw.NumFiles > 0 {
	filesfor1:
		for _, fi := range files {
			if fi.IsDir() {
				return fmt.Errorf("Rotation problem: File %s is directory", fi.Name())
			}
			ext := filepath.Ext(fi.Name())
			if (fname + ext) == fi.Name() {
				if ext == "" {
					continue filesfor1
				}
				ext = strings.Trim(ext, ".")
				num1, err := strconv.ParseInt(ext, 10, 64)
				num := int(num1)
				if (err != nil) && !os.IsNotExist(err) {
					continue filesfor1
				}
				if rw.NumFiles < num+1 { // unlink that shit
					err = os.Remove(path.Join(rw.dirpath, fi.Name()))
					if (err != nil) && !os.IsNotExist(err) {
						return err
					}
					continue filesfor1
				}
				sl = append(sl, num)
			}
		}
		sort.Slice(sl, func(i, j int) bool {
			return sl[i] > sl[j]
		})
		for _, num := range sl {
			err = os.Rename(
				path.Join(rw.dirpath, fname+"."+strconv.FormatInt(int64(num), 10)),
				path.Join(rw.dirpath, fname+"."+strconv.FormatInt(int64(num+1), 10)),
			)
			if err != nil {
				return err
			}
		}
		// FINAL OF PROCESS: swich file descriptor :-)
		// create firstfile there
		err = os.Rename(rw.Filename, rw.Filename+".1")
	} // end of if rw.NumFiles > 0
	// now we make new lock
	rnfile := true
	if rw.NumFiles == 0 {
		// here may be one fuckup: file was not renamed
		_, err := os.Stat(rw.Filename)
		rnfile = os.IsNotExist(err)
	}
	// right way first open file - not to make program wait while Write()
	if rnfile { // if file waas not deleted we really do not need reopen
		oldfile := rw.file
		newfile, err := rw.openWriteFileInt()
		if err != nil {
			return err
		}
		func() { // just isolate rw.writeMutex work here
			rw.writeMutex.Lock()
			defer rw.writeMutex.Unlock()
			// now file is opened. Just make save switch of them
			rw.file = newfile
		}()
		// aaaaand  when program started normal logging on new file close old the file
		oldfile.Close()
		// TODO: for case of numfiles > 0 add gzip|etc option here and do actions on test.log.1
	}
	return nil
}
