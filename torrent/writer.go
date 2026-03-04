package torrent

import (
	"fmt"
	"os"
	"path/filepath"

	"amish/metainfo"
)

// Writer handles writing verified piece data to the correct file(s) on disk.
type Writer struct {
	info    *metainfo.Info
	baseDir string
	files   []fileEntry
	handles map[string]*os.File
	mkdirs  map[string]bool
}

// NewWriter creates a Writer that saves files under baseDir.
func NewWriter(info *metainfo.Info, baseDir string) *Writer {
	w := &Writer{
		info:    info,
		baseDir: baseDir,
		handles: make(map[string]*os.File),
		mkdirs:  make(map[string]bool),
	}
	w.files = w.fileList()
	return w
}

// fileSegment maps a byte range to a file.
type fileSegment struct {
	Path   string
	Offset int64
	Length int64
}

// WritePiece writes a verified piece's data to the appropriate file(s).
func (w *Writer) WritePiece(index int, data []byte) error {
	segments := w.pieceSegments(index)

	offset := 0
	for _, seg := range segments {
		f, err := w.openFile(seg.Path)
		if err != nil {
			return err
		}

		_, err = f.WriteAt(data[offset:offset+int(seg.Length)], seg.Offset)
		if err != nil {
			return fmt.Errorf("write %s: %w", seg.Path, err)
		}

		offset += int(seg.Length)
	}

	return nil
}

// openFile returns a cached file handle, opening the file (and creating directories) if needed.
func (w *Writer) openFile(path string) (*os.File, error) {
	if f, ok := w.handles[path]; ok {
		return f, nil
	}

	dir := filepath.Dir(path)
	if !w.mkdirs[dir] {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
		w.mkdirs[dir] = true
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	w.handles[path] = f
	return f, nil
}

// Close closes all cached file handles.
func (w *Writer) Close() error {
	var first error
	for _, f := range w.handles {
		if err := f.Close(); err != nil && first == nil {
			first = err
		}
	}
	w.handles = nil
	return first
}

// pieceSegments returns the file segments that a piece maps to.
func (w *Writer) pieceSegments(index int) []fileSegment {
	pieceStart := int64(index) * w.info.PieceLength
	pieceLen := w.info.PieceSize(index)
	pieceEnd := pieceStart + pieceLen

	files := w.files

	var segments []fileSegment
	var fileOffset int64

	for _, f := range files {
		fileEnd := fileOffset + f.Length

		// Does this file overlap with the piece?
		if fileEnd <= pieceStart {
			fileOffset = fileEnd
			continue
		}
		if fileOffset >= pieceEnd {
			break
		}

		// Calculate the overlapping region.
		start := max(pieceStart, fileOffset)
		end := min(pieceEnd, fileEnd)

		segments = append(segments, fileSegment{
			Path:   f.Path,
			Offset: start - fileOffset,
			Length: end - start,
		})

		fileOffset = fileEnd
	}

	return segments
}

// fileEntry is a flattened file with absolute path.
type fileEntry struct {
	Path   string
	Length int64
}

func (w *Writer) fileList() []fileEntry {
	if w.info.Length > 0 {
		// Single file.
		return []fileEntry{{
			Path:   filepath.Join(w.baseDir, w.info.Name),
			Length: w.info.Length,
		}}
	}

	// Multi-file.
	entries := make([]fileEntry, len(w.info.Files))
	for i, f := range w.info.Files {
		parts := append([]string{w.baseDir, w.info.Name}, f.Path...)
		entries[i] = fileEntry{
			Path:   filepath.Join(parts...),
			Length: f.Length,
		}
	}
	return entries
}
