package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"amlogicmini/internal/archive"
	"amlogicmini/internal/profile"
	"amlogicmini/internal/progress"
)

// RawPlan describes a restore from a plain folder of <partition>.bin files
// (no manifest.json), as produced by other tools. Such images are not tied to
// this device, so only structural checks are possible.
type RawPlan struct {
	Input       string
	Partitions  []Partition
	PartialData bool
}

type RawSession struct{ Plan RawPlan }

var rawNames = []string{"boot_a", "boot_b", "system_a", "system_b", "data"}

// OpenRaw checks sizes, boot image headers and hashes of the five stock
// software partitions. It never touches the device.
func OpenRaw(ctx context.Context, dir string) (*RawSession, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	known := profile.A113X()
	page := int64(known.Geometry.PageSize)
	plan := RawPlan{Input: root}
	for _, name := range rawNames {
		var region profile.Region
		for _, r := range known.Partitions {
			if r.Name == name {
				region = r
			}
		}
		path := filepath.Join(root, name+".bin")
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("в папке stock нет файла %s.bin", name)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s.bin не обычный файл", name)
		}
		size := info.Size()
		if size <= 0 || size > int64(region.Size) || size%page != 0 {
			return nil, fmt.Errorf("%s.bin: размер %d байт не подходит (максимум %d, кратно %d)", name, size, region.Size, page)
		}
		if size < int64(region.Size) {
			if name != "data" || (int64(region.Size)-size)%0x20000 != 0 {
				return nil, fmt.Errorf("%s.bin короче нормы (%d из %d байт): такой образ снят с платы с плохими блоками, его не пишем", name, size, region.Size)
			}
			plan.PartialData = true
		}
		if name == "boot_a" || name == "boot_b" {
			magic, err := rawMagic(path)
			if err != nil {
				return nil, err
			}
			if !bytes.HasPrefix(magic, []byte("ANDROID!")) && !bytes.HasPrefix(magic, []byte("@AML")) {
				return nil, fmt.Errorf("%s.bin не похож на стоковый boot-образ: первые байты % x", name, magic)
			}
		}
		hash, _, err := archive.HashFile(ctx, path)
		if err != nil {
			return nil, err
		}
		plan.Partitions = append(plan.Partitions, Partition{Name: name, Offset: region.Offset, Bytes: size, ExpectedBytes: int64(region.Size), SHA256: hash})
	}
	return &RawSession{Plan: plan}, nil
}

func rawMagic(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, 8)
	n, err := io.ReadFull(f, b)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return b[:n], nil
}

// Apply writes every partition, reads it back and compares hashes, and only
// then restores the stock bootcmd. Reserved areas (keys, BBT, env, DDR),
// bootloader, tpl and misc are never written.
func (s *RawSession) Apply(ctx context.Context, d Device, notify progress.Notify) error {
	if notify != nil {
		notify(progress.Event{Stage: "check"})
	}
	if _, err := CheckConnection(ctx, d); err != nil {
		return err
	}
	for _, p := range s.Plan.Partitions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if notify != nil {
			notify(progress.Event{Stage: "source", Item: p.Name})
		}
		data, err := os.ReadFile(filepath.Join(s.Plan.Input, p.Name+".bin"))
		if err != nil {
			return err
		}
		if int64(len(data)) != p.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != p.SHA256 {
			return fmt.Errorf("файл %s.bin изменился после проверки", p.Name)
		}
		if notify != nil {
			notify(progress.Event{Stage: "write", Item: p.Name, Total: p.Bytes})
		}
		name, total := p.Name, p.Bytes
		if err = d.WritePartition(ctx, name, uint32(total), bytes.NewReader(data), func(done uint32) {
			if notify != nil {
				notify(progress.Event{Stage: "write", Item: name, Done: int64(done), Total: total})
			}
		}); err != nil {
			return fmt.Errorf("запись %s: %w", p.Name, err)
		}
		readback := sha256.New()
		n, err := d.ReadPartition(ctx, p.Name, uint32(p.Bytes), progress.Writer(readback, progress.Event{Stage: "verify", Item: p.Name, Total: p.Bytes}, notify))
		if err != nil {
			return fmt.Errorf("чтение %s для проверки: %w", p.Name, err)
		}
		if n != p.Bytes || fmt.Sprintf("%x", readback.Sum(nil)) != p.SHA256 {
			return fmt.Errorf("проверка после записи не совпала: %s", p.Name)
		}
	}
	if notify != nil {
		notify(progress.Event{Stage: "boot"})
	}
	if err := d.StockBoot(ctx); err != nil {
		return fmt.Errorf("не удалось вернуть стоковый bootcmd: %w", err)
	}
	return nil
}
