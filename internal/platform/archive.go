package platform

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxExtractedBinarySize = 128 << 20

// ExtractArchiveMember extracts one exact regular-file member. Callers choose
// the member from a signed lock manifest; no archive path is ever joined into
// the destination path.
func ExtractArchiveMember(archive, format, member, destination string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create extraction directory: %w", err)
	}
	switch format {
	case "tar.gz":
		return extractTarGZ(archive, member, destination, mode)
	case "zip":
		return extractZIP(archive, member, destination, mode)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func extractTarGZ(archive, member, destination string, mode os.FileMode) error {
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		if header.Name != member {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("archive member is not a regular file")
		}
		if header.Size < 0 || header.Size > maxExtractedBinarySize {
			return fmt.Errorf("archive member is too large")
		}
		return copyExtracted(destination, tarReader, mode)
	}
	return fmt.Errorf("archive member %q not found", member)
}

func extractZIP(archive, member, destination string, mode os.FileMode) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != member {
			continue
		}
		if file.FileInfo().IsDir() || file.Mode()&os.ModeType != 0 {
			return fmt.Errorf("archive member is not a regular file")
		}
		if file.UncompressedSize64 > maxExtractedBinarySize {
			return fmt.Errorf("archive member is too large")
		}
		contents, err := file.Open()
		if err != nil {
			return fmt.Errorf("open archive member: %w", err)
		}
		defer contents.Close()
		return copyExtracted(destination, contents, mode)
	}
	return fmt.Errorf("archive member %q not found", member)
}

func copyExtracted(destination string, source io.Reader, mode os.FileMode) error {
	contents, err := io.ReadAll(io.LimitReader(source, maxExtractedBinarySize+1))
	if err != nil {
		return fmt.Errorf("read archive member: %w", err)
	}
	if len(contents) > maxExtractedBinarySize {
		return fmt.Errorf("archive member is too large")
	}
	if err := AtomicWriteFile(destination, contents, mode); err != nil {
		return fmt.Errorf("write extracted member: %w", err)
	}
	return nil
}
