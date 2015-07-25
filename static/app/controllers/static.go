package controller

import (
	"github.com/revel/revel"
	"io"
	"net/http"
	"os"
	fpath "path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type OSStatic struct {
	*revel.Controller
}

func (c *OSStatic) setStatusIfNil(status int) {
	if c.Response.Status == 0 {
		c.Response.Status = status
	}
}

func (c OSStatic) Serve(prefix, filepath string) revel.Result {
	// Fix for #503.
	prefix = c.Params.Fixed.Get("prefix")
	if prefix == "" {
		return c.NotFound("")
	}

	return serve(c, prefix, filepath)
}

func (c OSStatic) ServeModule(moduleName, prefix, filepath string) revel.Result {
	// Fix for #503.
	prefix = c.Params.Fixed.Get("prefix")
	if prefix == "" {
		return c.NotFound("")
	}

	var basePath string
	for _, module := range revel.Modules {
		if module.Name == moduleName {
			basePath = module.Path
		}
	}

	absPath := fpath.Join(basePath, fpath.FromSlash(prefix))

	return serve(c, absPath, filepath)
}

// This method allows OSStatic serving of application files in a verified manner.
func serve(c OSStatic, prefix, filepath string) revel.Result {
	var basePath string
	if !fpath.IsAbs(prefix) {
		basePath = revel.BasePath
	}

	basePathPrefix := fpath.Join(basePath, fpath.FromSlash(prefix))
	fname := fpath.Join(basePathPrefix, fpath.FromSlash(filepath))
	// Verify the request file path is within the application's scope of access
	if !strings.HasPrefix(fname, basePathPrefix) {
		revel.WARN.Printf("Attempted to read file outside of base path: %s", fname)
		return c.NotFound("")
	}

	// Verify file path is accessible
	finfo, err := os.Stat(fname)
	if err != nil {
		if os.IsNotExist(err) || err.(*os.PathError).Err == syscall.ENOTDIR {
			revel.WARN.Printf("File not found (%s): %s ", fname, err)
			return c.NotFound("File not found")
		}
		revel.ERROR.Printf("Error trying to get fileinfo for '%s': %s", fname, err)
		return c.RenderError(err)
	}

	// Disallow directory listing
	if finfo.Mode().IsDir() {
		revel.WARN.Printf("Attempted directory listing of %s", fname)
		return c.Forbidden("Directory listing not allowed")
	}

	// Open request file path
	file, err := os.Open(fname)
	if err != nil {
		if os.IsNotExist(err) {
			revel.WARN.Printf("File not found (%s): %s ", fname, err)
			return c.NotFound("File not found")
		}
		revel.ERROR.Printf("Error opening '%s': %s", fname, err)
		return c.RenderError(err)
	}
	return c.RenderStaticFile(file)
}

type StaticBinaryResult struct {
	Reader  io.Reader
	Name    string
	Length  int64
	ModTime time.Time
}

func (r *StaticBinaryResult) Apply(req *revel.Request, resp *revel.Response) {
	// If we have a ReadSeeker, delegate to http.ServeContent
	if rs, ok := r.Reader.(io.ReadSeeker); ok {
		// http.ServeContent doesn't know about response.ContentType, so we set the respective header.
		if resp.ContentType != "" {
			resp.Out.Header().Set("Content-Type", resp.ContentType)
		} else {
			contentType := revel.ContentTypeByFilename(r.Name)
			resp.Out.Header().Set("Content-Type", contentType)
		}
		http.ServeContent(resp.Out, req.Request, r.Name, r.ModTime, rs)
	} else {
		// Else, do a simple io.Copy.
		if r.Length != -1 {
			resp.Out.Header().Set("Content-Length", strconv.FormatInt(r.Length, 10))
		}
		resp.WriteHeader(http.StatusOK, revel.ContentTypeByFilename(r.Name))
		io.Copy(resp.Out, r.Reader)
	}

	// Close the Reader if we can
	if v, ok := r.Reader.(io.Closer); ok {
		v.Close()
	}
}

func (c *OSStatic) RenderStaticFile(file *os.File) revel.Result {
	c.setStatusIfNil(http.StatusOK)

	var (
		modtime       = time.Now()
		fileInfo, err = file.Stat()
	)
	if err != nil {
		revel.WARN.Println("RenderFile error:", err)
	}
	if fileInfo != nil {
		modtime = fileInfo.ModTime()
	}
	return c.RenderStaticBinary(file, fpath.Base(file.Name()), modtime)
}

func (c *OSStatic) RenderStaticBinary(memfile io.Reader, filename string, modtime time.Time) revel.Result {
	c.setStatusIfNil(http.StatusOK)

	return &StaticBinaryResult{
		Reader:  memfile,
		Name:    filename,
		Length:  -1, // http.ServeContent gets the length itself unless memfile is a stream.
		ModTime: modtime,
	}
}
