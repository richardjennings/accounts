package importer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richardjennings/xls"
)

// maxWorkbook bounds one workbook read from an archive.
const maxWorkbook = 64 << 20

// ReadZip reads every .xls workbook in a zip archive. Each workbook's first
// sheet becomes a table named after the file without its extension. Other
// files, such as PDFs, are ignored.
func ReadZip(r io.ReaderAt, size int64) (Tables, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("import: not a zip archive: %w", err)
	}
	out := Tables{}
	for _, f := range zr.File {
		if !strings.EqualFold(path.Ext(f.Name), ".xls") || f.UncompressedSize64 > maxWorkbook {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxWorkbook))
		rc.Close()
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(path.Base(f.Name), path.Ext(f.Name))
		wb, err := xls.Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("import: %s: %w", f.Name, err)
		}
		if len(wb.Sheets) > 0 {
			out[name] = FromXLS(name, wb.Sheets[0])
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("import: the archive holds no .xls workbooks")
	}
	return out, nil
}

// ReadDir reads every .xls workbook in a directory, as ReadZip does.
func ReadDir(dir string) (Tables, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.xls"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := Tables{}
	for _, f := range files {
		wb, err := xls.OpenFile(f)
		if err != nil {
			return nil, fmt.Errorf("import: %s: %w", filepath.Base(f), err)
		}
		name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		if len(wb.Sheets) > 0 {
			out[name] = FromXLS(name, wb.Sheets[0])
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("import: no .xls workbooks in %s", dir)
	}
	return out, nil
}

// ReadZipFile reads a zip archive from disk.
func ReadZipFile(p string) (Tables, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return ReadZip(f, st.Size())
}
