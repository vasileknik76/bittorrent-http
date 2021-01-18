package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

type ds struct {
	t              *torrent.Torrent
	f              *torrent.File
	lastactivetime time.Time
}

func main() {
	c, _ := torrent.NewClient(nil)
	c.AddDHTNodes([]string{"router.bittorrent.com", "dht.transmissionbt.com", "router.utorrent.com"})
	defer c.Close()

	downloadingMap := make(map[string]*ds)

	go func() {
		for {
			for _, dw := range downloadingMap {
				if time.Now().After(dw.lastactivetime.Add(1 * time.Minute)) {
					os.Remove(dw.f.Path())
				}
			}
			time.Sleep(5 * time.Second)
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var t *torrent.Torrent
		var f *torrent.File
		var dsv *ds
		if downloadingMap[r.URL.Path] != nil {
			dsv = downloadingMap[r.URL.Path]
			t, f = dsv.t, dsv.f

		} else {
			re := regexp.MustCompile(`^/(?P<hash>\w+)/(?P<path>.*)$`)
			matches := re.FindStringSubmatch(r.URL.Path)
			hash := matches[1]
			path := matches[2]
			t, _ = c.AddMagnet("magnet:?xt=urn:btih:" + strings.ToUpper(hash))
			<-t.GotInfo()
			found := false
			for _, file := range t.Files() {
				fmt.Println(file.Path())
				if path == file.Path() {
					f = file
					found = true
					break
				}
			}
			if !found {
				w.Write([]byte("Not found"))
				w.WriteHeader(404)
				return
			}
			dsv = &ds{t: t, f: f}
			downloadingMap[r.URL.Path] = dsv
		}
		rangeHeader := r.Header.Get("Range")
		var start, end, pos int64

		if rangeHeader != "" {
			rangeHeader = strings.Replace(rangeHeader, "bytes=", "", 1)
			vals := strings.Split(rangeHeader, "-")
			start, _ = strconv.ParseInt(vals[0], 10, 64)
			end, _ = strconv.ParseInt(vals[1], 10, 64)
		}
		fmt.Printf("R %s Range %s %d-%d\n", r.URL.String(), rangeHeader, start, end)
		reader := f.NewReader()
		reader.Seek(start, io.SeekStart)
		pos = start
		w.Header().Add("Accept-Ranges", "bytes")
		w.Header().Add("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, f.Length()))
		if start != 0 {
			w.WriteHeader(206)
		} else {
			w.WriteHeader(200)
		}
		buf := make([]byte, 1024)
		needClose := false
		for {
			// read a chunk
			n, err := reader.Read(buf)
			if end != 0 && pos+int64(n) > end {
				n = int(end - pos)
				needClose = true
			}
			if err != nil && err != io.EOF {
				panic(err)
			}
			if n == 0 {
				break
			}

			// write a chunk
			if _, err := w.Write(buf[:n]); err != nil {
				dsv.lastactivetime = time.Now()
				reader.Close()
				return
			}
			dsv.lastactivetime = time.Now()
			pos += int64(n)

			if needClose {
				reader.Close()
				return
			}
		}
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
