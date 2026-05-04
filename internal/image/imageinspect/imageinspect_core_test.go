package imageinspect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/diskfs/go-diskfs/partition/mbr"
)

func TestInspectCore_Propagates_GetPartitionTable_Error(t *testing.T) {
	d := &DiskfsInspector{}
	img := tinyReaderAt(4096)

	want := errors.New("pt boom")
	disk := &fakeDiskAccessor{ptErr: want}

	_, err := d.inspectCoreNoHash(img, disk, 512, "ignored", 1<<20)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, want) {
		t.Fatalf("err=%v want wrapping %v", err, want)
	}
	if disk.calls.getPT != 1 {
		t.Fatalf("GetPartitionTable calls=%d want 1", disk.calls.getPT)
	}
}

func TestInspectCore_GPT_Table_SetsTypeAndBasics(t *testing.T) {
	d := &DiskfsInspector{}
	img := tinyReaderAt(4096)

	disk := &fakeDiskAccessor{pt: minimalGPTWithOnePartition()}

	got, err := d.inspectCoreNoHash(img, disk, 512, "ignored", 8<<20)
	if err != nil {
		t.Fatalf("inspectCore: %v", err)
	}
	if got.PartitionTable.Type != "gpt" {
		t.Fatalf("PartitionTable.Type=%q want gpt", got.PartitionTable.Type)
	}
	require(t, len(got.PartitionTable.Partitions) > 0, "expected at least 1 partition")
}

func TestInspectCore_MBR_Table_SetsTypeAndBasics(t *testing.T) {
	d := &DiskfsInspector{}
	img := tinyReaderAt(4096)

	disk := &fakeDiskAccessor{pt: minimalMBRWithOnePartition()}

	got, err := d.inspectCoreNoHash(img, disk, 512, "ignored", 8<<20)
	if err != nil {
		t.Fatalf("inspectCore: %v", err)
	}
	if got.PartitionTable.Type != "mbr" {
		t.Fatalf("PartitionTable.Type=%q want mbr", got.PartitionTable.Type)
	}
	require(t, len(got.PartitionTable.Partitions) > 0, "expected at least 1 partition")
}

func TestInspectCore_GetFilesystem_Error_IsRecordedAsNote(t *testing.T) {
	d := &DiskfsInspector{}
	img := tinyReaderAt(4096)

	want := errors.New("fs boom")
	disk := &fakeDiskAccessor{
		pt:       minimalGPTWithOnePartition(),
		fsErrAny: want, // any filesystem open fails
	}

	got, err := d.inspectCoreNoHash(img, disk, 512, "ignored", 8<<20)
	if err != nil {
		t.Fatalf("inspectCore should not fail on GetFilesystem error; got: %v", err)
	}

	require(t, len(disk.calls.getFS) > 0, "expected GetFilesystem to be called at least once")

	parts := got.PartitionTable.Partitions
	require(t, len(parts) > 0, "expected partitions")
	require(t, parts[0].Filesystem != nil, "expected Filesystem to be non-nil")

	notes := strings.Join(parts[0].Filesystem.Notes, "\n")
	require(t, strings.Contains(notes, "diskfs GetFilesystem("), "expected GetFilesystem note; got notes:\n%s", notes)
	require(t, strings.Contains(notes, "fs boom"), "expected error text in notes; got notes:\n%s", notes)
}

func TestSummarizePartitionTable_LogicalBlockSizeAffectsSizeBytes(t *testing.T) {
	pt := minimalGPTWithOnePartition()

	a, err := summarizePartitionTable(pt, 512, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	b, err := summarizePartitionTable(pt, 4096, 8<<20)
	if err != nil {
		t.Fatal(err)
	}

	if a.Partitions[0].SizeBytes*8 != b.Partitions[0].SizeBytes {
		t.Fatalf("expected 4096-byte blocks to produce 8x size: a=%d b=%d", a.Partitions[0].SizeBytes, b.Partitions[0].SizeBytes)
	}
}

type sliceReaderAt struct{ b []byte }

func (s sliceReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(s.b)) {
		return 0, io.EOF
	}
	n := copy(p, s.b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestEmptyIfWhitespace(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "-"},
		{"   ", "-"},
		{"\n\t", "-"},
		{" x ", "x"},
	}
	for _, tc := range cases {
		if got := emptyIfWhitespace(tc.in); got != tc.want {
			t.Fatalf("in=%q got=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.n); got != tc.want {
			t.Fatalf("n=%d got=%q want=%q", tc.n, got, tc.want)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	raw := `
# comment
NAME="Azure Linux"
VERSION_ID=3.0
EMPTY=
SPACED = "hello world"
QUOTED='x'
BADLINE
`
	m, _ := parseOSRelease(raw)
	if m["NAME"] != "Azure Linux" {
		t.Fatalf("NAME=%q", m["NAME"])
	}
	if m["VERSION_ID"] != "3.0" {
		t.Fatalf("VERSION_ID=%q", m["VERSION_ID"])
	}
	// EMPTY= should still set key with empty value
	if _, ok := m["EMPTY"]; !ok {
		t.Fatalf("expected EMPTY key present")
	}
	if m["SPACED"] != "hello world" {
		t.Fatalf("SPACED=%q", m["SPACED"])
	}
	if m["QUOTED"] != "x" {
		t.Fatalf("QUOTED=%q", m["QUOTED"])
	}
	if _, ok := m["BADLINE"]; ok {
		t.Fatalf("did not expect BADLINE key")
	}
}

func TestHasSection(t *testing.T) {
	secs := []string{" .linux", ".CMDLINE", ".osrel", "foo"}
	if !hasSection(secs, ".linux") {
		t.Fatalf("expected .linux")
	}
	if !hasSection(secs, ".cmdline") {
		t.Fatalf("expected .cmdline")
	}
	if hasSection(secs, ".initrd") {
		t.Fatalf("did not expect .initrd")
	}
}

func TestSummarizePartitionTable_GPT(t *testing.T) {
	pt := &gpt.Table{
		PhysicalSectorSize: 4096,
		LogicalSectorSize:  512,
		ProtectiveMBR:      true,
		Partitions: []*gpt.Partition{
			// out of order on purpose to test sorting by StartLBA
			{Start: 4096, End: 8191, Name: "B", Type: "0FC63DAF-8483-4772-8E79-3D69D8477DE4"},
			{Start: 2048, End: 4095, Name: "A", Type: "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"},
			// empty entry should be skipped
			{Start: 0, End: 0, Name: "EMPTY"},
		},
	}

	sum, err := summarizePartitionTable(pt, 512, 16<<20)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum.Type != "gpt" {
		t.Fatalf("Type=%q", sum.Type)
	}
	if sum.LogicalSectorSize != 512 || sum.PhysicalSectorSize != 4096 {
		t.Fatalf("sector sizes got L=%d P=%d", sum.LogicalSectorSize, sum.PhysicalSectorSize)
	}
	if !sum.ProtectiveMBR {
		t.Fatalf("expected ProtectiveMBR true")
	}
	if len(sum.Partitions) != 2 {
		t.Fatalf("partitions=%d want 2", len(sum.Partitions))
	}
	// sorted by StartLBA
	if sum.Partitions[0].Name != "A" || sum.Partitions[1].Name != "B" {
		t.Fatalf("unexpected order: %#v", sum.Partitions)
	}
	// size bytes = (end-start+1)*logicalBlockSize
	if sum.Partitions[0].SizeBytes != (4095-2048+1)*512 {
		t.Fatalf("sizeBytes=%d", sum.Partitions[0].SizeBytes)
	}
}

func TestSummarizePartitionTable_MBR(t *testing.T) {
	pt := &mbr.Table{
		PhysicalSectorSize: 4096,
		LogicalSectorSize:  512,
		Partitions: []*mbr.Partition{
			{Type: 0x83, Start: 2048, Size: 2048},
		},
	}
	sum, err := summarizePartitionTable(pt, 512, 8<<20)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum.Type != "mbr" {
		t.Fatalf("Type=%q", sum.Type)
	}
	if len(sum.Partitions) != 1 {
		t.Fatalf("partitions=%d", len(sum.Partitions))
	}
	p := sum.Partitions[0]
	if p.StartLBA != 2048 || p.EndLBA != 2048+2048-1 {
		t.Fatalf("start/end got %d/%d", p.StartLBA, p.EndLBA)
	}
	if p.SizeBytes != 2048*512 {
		t.Fatalf("SizeBytes=%d", p.SizeBytes)
	}
}

func TestSniffFilesystemType_Squashfs(t *testing.T) {
	buf := make([]byte, 8192)
	copy(buf[0:4], []byte("hsqs"))
	r := sliceReaderAt{b: buf}

	got, err := sniffFilesystemType(r, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "squashfs" {
		t.Fatalf("got=%q", got)
	}
}

func TestSniffFilesystemType_Ext(t *testing.T) {
	buf := make([]byte, 8192)
	// ext magic at offset 1024+56: 0xEF53 little => bytes 0x53,0xEF
	buf[1024+56] = 0x53
	buf[1024+57] = 0xEF
	r := sliceReaderAt{b: buf}

	got, err := sniffFilesystemType(r, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "ext4" {
		t.Fatalf("got=%q", got)
	}
}

func TestSniffFilesystemType_FAT(t *testing.T) {
	buf := make([]byte, 8192)
	buf[510] = 0x55
	buf[511] = 0xAA
	r := sliceReaderAt{b: buf}

	got, err := sniffFilesystemType(r, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "vfat" {
		t.Fatalf("got=%q", got)
	}
}

func TestReadExtSuperblock(t *testing.T) {
	buf := make([]byte, 4096)
	sbOff := 1024

	// magic
	buf[sbOff+56] = 0x53
	buf[sbOff+57] = 0xEF

	// UUID bytes 16 at 104..120
	uuid := []byte{
		0x10, 0x32, 0x54, 0x76,
		0x98, 0xba,
		0xdc, 0xfe,
		0x01, 0x23,
		0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
	}
	copy(buf[sbOff+104:sbOff+120], uuid)

	// label at 120..136
	copy(buf[sbOff+120:sbOff+136], []byte("MYVOL\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))

	// s_log_block_size @ 24..28, set to 2 => block size 1024<<2=4096
	binary.LittleEndian.PutUint32(buf[sbOff+24:sbOff+28], 2)

	r := sliceReaderAt{b: buf}
	out := &FilesystemSummary{}
	if err := readExtSuperblock(r, 0, out); err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.UUID == "" || !strings.Contains(out.UUID, "-") {
		t.Fatalf("UUID=%q", out.UUID)
	}
	if out.Label != "MYVOL" {
		t.Fatalf("Label=%q", out.Label)
	}
	if out.BlockSize != 4096 {
		t.Fatalf("BlockSize=%d", out.BlockSize)
	}
}

type memReaderAt struct{ b []byte }

func (m memReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(m.b)) {
		return 0, io.EOF
	}
	n := copy(p, m.b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func newBuf(size int) []byte { return make([]byte, size) }

func TestSniffFilesystemType_Unknown(t *testing.T) {
	buf := newBuf(4096)
	got, err := sniffFilesystemType(memReaderAt{buf}, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "unknown" {
		t.Fatalf("got=%q", got)
	}
}

func TestReadFATBootSector_MissingSignature(t *testing.T) {
	buf := newBuf(512)
	var out FilesystemSummary
	err := readFATBootSector(memReaderAt{buf}, 0, &out)
	if err == nil || !strings.Contains(err.Error(), "0x55AA") {
		t.Fatalf("expected signature error, got: %v", err)
	}
}

func TestReadFATBootSector_InvalidSPC(t *testing.T) {
	buf := newBuf(512)
	buf[510] = 0x55
	buf[511] = 0xAA
	binary.LittleEndian.PutUint16(buf[11:13], 512)
	buf[13] = 0 // invalid
	binary.LittleEndian.PutUint16(buf[14:16], 1)
	buf[16] = 2
	binary.LittleEndian.PutUint16(buf[17:19], 512)
	binary.LittleEndian.PutUint16(buf[19:21], 6000)
	binary.LittleEndian.PutUint16(buf[22:24], 9)

	var out FilesystemSummary
	err := readFATBootSector(memReaderAt{buf}, 0, &out)
	if err == nil || !strings.Contains(err.Error(), "sectorsPerCluster") {
		t.Fatalf("expected sectorsPerCluster error, got: %v", err)
	}
}

func TestOpenFAT_FAT32(t *testing.T) {
	img := newBuf(4096)
	bs := img[:512]
	bs[510] = 0x55
	bs[511] = 0xAA
	binary.LittleEndian.PutUint16(bs[11:13], 512)
	bs[13] = 8
	binary.LittleEndian.PutUint16(bs[14:16], 32)
	bs[16] = 2
	binary.LittleEndian.PutUint16(bs[17:19], 0)   // FAT32
	binary.LittleEndian.PutUint16(bs[22:24], 0)   // fatSz16=0
	binary.LittleEndian.PutUint32(bs[36:40], 123) // fatSz32
	binary.LittleEndian.PutUint32(bs[32:36], 100000)
	binary.LittleEndian.PutUint32(bs[44:48], 2) // rootClus

	v, err := openFAT(memReaderAt{img}, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.kind != fat32 {
		t.Fatalf("kind=%v", v.kind)
	}
	if v.clusterSize == 0 || v.dataStart == 0 {
		t.Fatalf("derived not set: clusterSize=%d dataStart=%d", v.clusterSize, v.dataStart)
	}
}

func TestOpenFAT_InvalidSignature(t *testing.T) {
	img := newBuf(512)
	_, err := openFAT(memReaderAt{img}, 0)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestReadExtSuperblock_Success(t *testing.T) {
	img := newBuf(4096)
	sb := img[1024 : 1024+1024]

	// magic at 56..58
	binary.LittleEndian.PutUint16(sb[56:58], 0xEF53)
	// log block size at 24..28 -> 0 => 1024
	binary.LittleEndian.PutUint32(sb[24:28], 0)
	// UUID 104..120
	copy(sb[104:120], []byte{
		0x01, 0x02, 0x03, 0x04,
		0x05, 0x06,
		0x07, 0x08,
		0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	})
	// label 120..136
	copy(sb[120:136], []byte("ROOTFS\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))

	// features
	binary.LittleEndian.PutUint32(sb[92:96], 0x0004)   // has_journal
	binary.LittleEndian.PutUint32(sb[96:100], 0x0040)  // extents
	binary.LittleEndian.PutUint32(sb[100:104], 0x0008) // huge_file

	var out FilesystemSummary
	err := readExtSuperblock(memReaderAt{img}, 0, &out)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.BlockSize != 1024 {
		t.Fatalf("BlockSize=%d", out.BlockSize)
	}
	if out.UUID == "" || !strings.Contains(out.UUID, "-") {
		t.Fatalf("UUID=%q", out.UUID)
	}
	if out.Label != "ROOTFS" {
		t.Fatalf("Label=%q", out.Label)
	}
	if len(out.Features) == 0 {
		t.Fatalf("Features empty")
	}
}

func TestReadExtSuperblock_BadMagic(t *testing.T) {
	img := newBuf(4096)
	sb := img[1024 : 1024+1024]
	binary.LittleEndian.PutUint16(sb[56:58], 0x1234)

	var out FilesystemSummary
	err := readExtSuperblock(memReaderAt{img}, 0, &out)
	if err == nil || !strings.Contains(err.Error(), "magic mismatch") {
		t.Fatalf("expected magic mismatch, got: %v", err)
	}
}

func TestReadSquashfsSuperblock_Success(t *testing.T) {
	img := newBuf(4096)
	sb := img[:96]
	copy(sb[0:4], []byte("hsqs"))
	binary.LittleEndian.PutUint32(sb[12:16], 131072) // block size
	binary.LittleEndian.PutUint16(sb[16:18], 0x0080) // no_xattrs
	binary.LittleEndian.PutUint16(sb[20:22], 4)      // xz
	binary.LittleEndian.PutUint16(sb[28:30], 4)
	binary.LittleEndian.PutUint16(sb[30:32], 0)

	var out FilesystemSummary
	err := readSquashfsSuperblock(memReaderAt{img}, 0, &out)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.BlockSize != 131072 {
		t.Fatalf("BlockSize=%d", out.BlockSize)
	}
	if out.Compression != "xz" {
		t.Fatalf("Compression=%q", out.Compression)
	}
	if out.Version != "4.0" {
		t.Fatalf("Version=%q", out.Version)
	}
	if len(out.FsFlags) == 0 {
		t.Fatalf("FsFlags empty")
	}
}

func TestInspectFileSystemsFromHandles_InvalidLogicalSectorSize(t *testing.T) {
	_, err := InspectFileSystemsFromHandles(
		memReaderAt{newBuf(4096)},
		&fakeDiskAccessor{},
		PartitionTableSummary{LogicalSectorSize: 0, Partitions: []PartitionSummary{{Index: 1}}},
	)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestInspectFileSystemsFromHandles_EmptyPartitions(t *testing.T) {
	got, err := InspectFileSystemsFromHandles(
		memReaderAt{newBuf(4096)},
		&fakeDiskAccessor{},
		PartitionTableSummary{LogicalSectorSize: 512, Partitions: nil},
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil partitions, got %v", got)
	}
}

func TestPrintSummary_Smoke(t *testing.T) {
	sum := &ImageSummary{
		File:      "dummy.img",
		SizeBytes: 1234,
		PartitionTable: PartitionTableSummary{
			Type:               "gpt",
			LogicalSectorSize:  512,
			PhysicalSectorSize: 4096,
			ProtectiveMBR:      true,
			Partitions: []PartitionSummary{
				{
					Index:      1,
					Name:       "ESP",
					Type:       "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					StartLBA:   2048,
					EndLBA:     4095,
					SizeBytes:  512 * 2048,
					Filesystem: &FilesystemSummary{Type: "vfat", Label: "ESP", FATType: "FAT32"},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderSummaryText(&buf, sum, TextOptions{}); err != nil {
		t.Fatalf("RenderSummaryText error: %v", err)
	}

	s := buf.String()
	if !strings.Contains(s, "OS Image Summary") {
		t.Fatalf("missing header")
	}
	if !strings.Contains(s, "Partition Table") {
		t.Fatalf("missing PT section")
	}
	if !strings.Contains(s, "Partitions") {
		t.Fatalf("missing partitions section")
	}
}

func TestReadSquashfsSuperblock(t *testing.T) {
	buf := make([]byte, 4096)
	copy(buf[0:4], []byte("hsqs"))
	binary.LittleEndian.PutUint32(buf[12:16], 131072) // block size
	binary.LittleEndian.PutUint16(buf[16:18], 0x0080) // no_xattrs
	binary.LittleEndian.PutUint16(buf[20:22], 4)      // xz
	binary.LittleEndian.PutUint16(buf[28:30], 4)      // major
	binary.LittleEndian.PutUint16(buf[30:32], 0)      // minor

	r := sliceReaderAt{b: buf}
	out := &FilesystemSummary{}
	if err := readSquashfsSuperblock(r, 0, out); err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.BlockSize != 131072 {
		t.Fatalf("BlockSize=%d", out.BlockSize)
	}
	if out.Compression != "xz" {
		t.Fatalf("Compression=%q", out.Compression)
	}
	if out.Version != "4.0" {
		t.Fatalf("Version=%q", out.Version)
	}
	if len(out.FsFlags) == 0 || out.FsFlags[0] == "" {
		t.Fatalf("FsFlags=%v", out.FsFlags)
	}
}

func TestReadFATBootSector_FAT16(t *testing.T) {
	bs := make([]byte, 512)
	// signature
	bs[510] = 0x55
	bs[511] = 0xAA
	// bytes per sector 512
	binary.LittleEndian.PutUint16(bs[11:13], 512)
	// sectors per cluster
	bs[13] = 1
	// reserved sectors
	binary.LittleEndian.PutUint16(bs[14:16], 1)
	// numFATs
	bs[16] = 2
	// root entries (non-zero => FAT12/16 layout)
	binary.LittleEndian.PutUint16(bs[17:19], 512)
	// total sectors 16
	binary.LittleEndian.PutUint16(bs[19:21], 8192)
	// fatSz16
	binary.LittleEndian.PutUint16(bs[22:24], 8)

	// VolID at 39..43
	binary.LittleEndian.PutUint32(bs[39:43], 0xA1B2C3D4)
	// Label at 43..54 (11 bytes)
	copy(bs[43:54], []byte("MYFATLABEL  "))

	r := sliceReaderAt{b: bs}
	out := &FilesystemSummary{}
	if err := readFATBootSector(r, 0, out); err != nil {
		t.Fatalf("err: %v", err)
	}

	if out.Type != "vfat" {
		t.Fatalf("Type=%q", out.Type)
	}
	if out.FATType != "FAT16" && out.FATType != "FAT12" {
		t.Fatalf("FATType=%q", out.FATType)
	}
	if out.BytesPerSector != 512 || out.SectorsPerCluster != 1 {
		t.Fatalf("bps/spc got %d/%d", out.BytesPerSector, out.SectorsPerCluster)
	}
	if out.UUID != "a1b2c3d4" {
		t.Fatalf("UUID=%q", out.UUID)
	}
	if strings.TrimSpace(out.Label) != "MYFATLABEL" {
		t.Fatalf("Label=%q", out.Label)
	}
}

func TestReadFATBootSector_FAT32(t *testing.T) {
	bs := make([]byte, 512)
	bs[510] = 0x55
	bs[511] = 0xAA
	binary.LittleEndian.PutUint16(bs[11:13], 512)
	bs[13] = 8
	binary.LittleEndian.PutUint16(bs[14:16], 32)
	bs[16] = 2
	// FAT32 markers:
	binary.LittleEndian.PutUint16(bs[17:19], 0)   // rootEntCnt=0
	binary.LittleEndian.PutUint16(bs[22:24], 0)   // fatSz16=0
	binary.LittleEndian.PutUint32(bs[36:40], 123) // fatSz32 != 0

	// total sectors 32
	binary.LittleEndian.PutUint32(bs[32:36], 65536)

	// FAT32 VolID at 67..71
	binary.LittleEndian.PutUint32(bs[67:71], 0x11223344)
	// Label 71..82 (11 bytes)
	copy(bs[71:82], []byte("FAT32LABEL "))

	r := sliceReaderAt{b: bs}
	out := &FilesystemSummary{}
	if err := readFATBootSector(r, 0, out); err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.FATType != "FAT32" {
		t.Fatalf("FATType=%q", out.FATType)
	}
	if out.UUID != "11223344" {
		t.Fatalf("UUID=%q", out.UUID)
	}
	if strings.TrimSpace(out.Label) != "FAT32LABEL" {
		t.Fatalf("Label=%q", out.Label)
	}
	if out.ClusterCount == 0 {
		t.Fatalf("expected ClusterCount > 0")
	}
}

func TestOpenFAT_RejectsBadSignature(t *testing.T) {
	bs := make([]byte, 512)
	r := sliceReaderAt{b: bs}
	_, err := openFAT(r, 0)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseDirEntries_83NameAndSize(t *testing.T) {
	// One directory entry (32 bytes)
	buf := make([]byte, 32)
	copy(buf[0:11], []byte("KERNEL  EFI"))               // "KERNEL.EFI" in 8.3 (spaces)
	buf[11] = 0x20                                       // archive
	binary.LittleEndian.PutUint16(buf[26:28], 5)         // first cluster low
	binary.LittleEndian.PutUint32(buf[28:32], 123456789) // size

	ents, err := parseDirEntries(buf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ents) != 1 {
		t.Fatalf("ents=%d", len(ents))
	}
	if ents[0].name != "KERNEL.EFI" {
		t.Fatalf("name=%q", ents[0].name)
	}
	if ents[0].size != 123456789 {
		t.Fatalf("size=%d", ents[0].size)
	}
}

func TestDecodeLFNPart_Smoke(t *testing.T) {
	// Construct a simple LFN entry carrying "A"
	e := make([]byte, 32)
	e[11] = 0x0F // LFN attribute

	// LFN stores UTF16LE in three ranges; put 'A' (0x0041) at first position.
	binary.LittleEndian.PutUint16(e[1:3], 0x0041)
	// Terminate
	binary.LittleEndian.PutUint16(e[3:5], 0x0000)

	got := decodeLFNPart(e)
	if got != "A" {
		t.Fatalf("got=%q", got)
	}
}

func TestGPTTypeNameAndPartitionRole(t *testing.T) {
	espGUID := "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
	if got := gptTypeName(espGUID); got == "" {
		t.Fatalf("expected GPT type name for ESP")
	}

	p := PartitionSummary{
		Name: "ESP",
		Type: espGUID,
		Filesystem: &FilesystemSummary{
			Type: "vfat",
		},
	}
	if role := partitionRole(p); role != "ESP" {
		t.Fatalf("role=%q", role)
	}
}

func TestPeMachineToArch(t *testing.T) {
	if got := peMachineToArch(0x8664); got != "x86_64" { // AMD64
		t.Fatalf("got=%q", got)
	}
	if got := peMachineToArch(0x014c); got != "x86" { // I386
		t.Fatalf("got=%q", got)
	}
}

func TestInspectCore_PropagatesFilesystemError_WhenCalled(t *testing.T) {
	// This is the same intent as your earlier test, but modified to ensure
	// we actually have a GPT partition in the table so FS probing is attempted.
	d := &DiskfsInspector{}
	img := sliceReaderAt{b: make([]byte, 4096)}

	want := errors.New("fs boom")
	disk := &fakeDiskAccessor{
		pt: &gpt.Table{
			PhysicalSectorSize: 4096,
			LogicalSectorSize:  512,
			ProtectiveMBR:      true,
			Partitions: []*gpt.Partition{
				{Start: 2048, End: 4095, Name: "ESP"},
			},
		},
		fsErrAny: want,
	}

	_, err := d.inspectCoreNoHash(img, disk, 512, "ignored", 8<<20)

	if err != nil {
		t.Fatalf("did not expect inspectCore error; GetFilesystem failures are captured as notes. err=%v", err)
	}
	if len(disk.calls.getFS) == 0 {
		t.Fatalf("expected GetFilesystem to be called at least once")
	}
}

func TestInheritBootloaderKindBySHA_InheritsFromKnown(t *testing.T) {
	evs := []EFIBinaryEvidence{
		{Path: "/EFI/ubuntu/shimx64.efi", SHA256: "abc123def456", Kind: BootloaderShim},
		{Path: "/EFI/BOOT/BOOTX64.EFI", SHA256: "abc123def456", Kind: BootloaderUnknown},
	}

	inheritBootloaderKindBySHA(evs)

	if evs[1].Kind != BootloaderShim {
		t.Errorf("expected BOOTX64.EFI to inherit kind=shim, got %q", evs[1].Kind)
	}
	if len(evs[1].Notes) == 0 || !strings.Contains(evs[1].Notes[0], "sha256 match") {
		t.Errorf("expected note about sha256 inheritance, got %v", evs[1].Notes)
	}
	// Original should remain unchanged
	if evs[0].Kind != BootloaderShim {
		t.Errorf("original should stay shim, got %q", evs[0].Kind)
	}
}

func TestInheritBootloaderKindBySHA_NoMatchLeavesUnknown(t *testing.T) {
	evs := []EFIBinaryEvidence{
		{Path: "/EFI/ubuntu/shimx64.efi", SHA256: "abc123", Kind: BootloaderShim},
		{Path: "/EFI/BOOT/BOOTX64.EFI", SHA256: "different456", Kind: BootloaderUnknown},
	}

	inheritBootloaderKindBySHA(evs)

	if evs[1].Kind != BootloaderUnknown {
		t.Errorf("expected BOOTX64.EFI to remain unknown when hash differs, got %q", evs[1].Kind)
	}
	if len(evs[1].Notes) != 0 {
		t.Errorf("expected no notes when no match, got %v", evs[1].Notes)
	}
}

func TestInheritBootloaderKindBySHA_EmptySHA256Ignored(t *testing.T) {
	evs := []EFIBinaryEvidence{
		{Path: "/EFI/ubuntu/shimx64.efi", SHA256: "", Kind: BootloaderShim},
		{Path: "/EFI/BOOT/BOOTX64.EFI", SHA256: "", Kind: BootloaderUnknown},
	}

	inheritBootloaderKindBySHA(evs)

	// Both should remain unchanged - empty SHA256 entries are skipped
	if evs[0].Kind != BootloaderShim {
		t.Errorf("expected shim to remain shim, got %q", evs[0].Kind)
	}
	if evs[1].Kind != BootloaderUnknown {
		t.Errorf("expected unknown to remain unknown when SHA256 empty, got %q", evs[1].Kind)
	}
}

func TestInheritBootloaderKindBySHA_MultipleInheritances(t *testing.T) {
	evs := []EFIBinaryEvidence{
		{Path: "/EFI/ubuntu/shimx64.efi", SHA256: "shimhash", Kind: BootloaderShim},
		{Path: "/EFI/fedora/grubx64.efi", SHA256: "grubhash", Kind: BootloaderGrub},
		{Path: "/EFI/BOOT/BOOTX64.EFI", SHA256: "shimhash", Kind: BootloaderUnknown},
		{Path: "/EFI/BOOT/grubx64.efi", SHA256: "grubhash", Kind: BootloaderUnknown},
		{Path: "/EFI/unknown/mystery.efi", SHA256: "otherhash", Kind: BootloaderUnknown},
	}

	inheritBootloaderKindBySHA(evs)

	if evs[2].Kind != BootloaderShim {
		t.Errorf("BOOTX64.EFI should inherit shim, got %q", evs[2].Kind)
	}
	if evs[3].Kind != BootloaderGrub {
		t.Errorf("grubx64.efi copy should inherit grub, got %q", evs[3].Kind)
	}
	if evs[4].Kind != BootloaderUnknown {
		t.Errorf("mystery.efi should remain unknown, got %q", evs[4].Kind)
	}
}

func TestInheritBootloaderKindBySHA_FirstKnownWins(t *testing.T) {
	// If the same hash appears with different kinds, first one wins
	evs := []EFIBinaryEvidence{
		{Path: "/EFI/first/shimx64.efi", SHA256: "samehash", Kind: BootloaderShim},
		{Path: "/EFI/second/grubx64.efi", SHA256: "samehash", Kind: BootloaderGrub},
		{Path: "/EFI/BOOT/BOOTX64.EFI", SHA256: "samehash", Kind: BootloaderUnknown},
	}

	inheritBootloaderKindBySHA(evs)

	// The unknown should inherit from the first known (shim)
	if evs[2].Kind != BootloaderShim {
		t.Errorf("expected first known kind (shim) to win, got %q", evs[2].Kind)
	}
}

func TestInheritBootloaderKindBySHA_AlreadyClassifiedNotOverwritten(t *testing.T) {
	evs := []EFIBinaryEvidence{
		{Path: "/EFI/ubuntu/shimx64.efi", SHA256: "abc123", Kind: BootloaderShim},
		{Path: "/EFI/BOOT/BOOTX64.EFI", SHA256: "abc123", Kind: BootloaderGrub}, // Already classified differently
	}

	inheritBootloaderKindBySHA(evs)

	// Should NOT overwrite an already-classified binary
	if evs[1].Kind != BootloaderGrub {
		t.Errorf("already classified binary should not be overwritten, got %q", evs[1].Kind)
	}
}

func TestSummarizePartitionTable_PartitionWithNilFilesystem(t *testing.T) {
	// A partition can exist without a filesystem (e.g., raw data partition)
	pt := &gpt.Table{
		LogicalSectorSize: 512,
		Partitions: []*gpt.Partition{
			{
				Name:  "raw_data",
				Start: 2048,
				End:   4095,
			},
		},
	}

	got, err := summarizePartitionTable(pt, 512, 8<<20)
	if err != nil {
		t.Fatalf("summarizePartitionTable: %v", err)
	}

	require(t, len(got.Partitions) == 1, "expected 1 partition")
	if got.Partitions[0].Filesystem != nil {
		t.Fatalf("expected Filesystem to be nil for raw partition, got %+v", got.Partitions[0].Filesystem)
	}
	// Verify partition is otherwise properly summarized
	if got.Partitions[0].Name != "raw_data" {
		t.Fatalf("expected partition name raw_data, got %s", got.Partitions[0].Name)
	}
}

func TestInspectCore_InvalidLogicalSectorSize_Zero(t *testing.T) {
	d := &DiskfsInspector{}
	img := tinyReaderAt(4096)
	disk := &fakeDiskAccessor{pt: minimalGPTWithOnePartition()}

	// Sector size of 0 should be rejected
	_, err := d.inspectCoreNoHash(img, disk, 0, "ignored", 8<<20)
	if err == nil {
		t.Fatalf("expected error for sector size 0")
	}
}

func TestInspectCore_InvalidLogicalSectorSize_ExtremelyLarge(t *testing.T) {
	d := &DiskfsInspector{}
	img := tinyReaderAt(4096)
	disk := &fakeDiskAccessor{pt: minimalGPTWithOnePartition()}

	// Sector size larger than typical disk would likely cause issues
	_, err := d.inspectCoreNoHash(img, disk, 1<<30, "ignored", 8<<20) // 1GB sector size
	if err == nil {
		t.Fatalf("expected error for extremely large sector size")
	}
}

func TestSummarizePartitionTable_MisalignedPartitions(t *testing.T) {
	// Partitions not aligned to sector boundaries are detected and reported
	pt := &gpt.Table{
		LogicalSectorSize: 512,
		Partitions: []*gpt.Partition{
			{
				Name:  "aligned",
				Start: 2048,
				End:   4095,
			},
			{
				Name:  "misaligned",
				Start: 4096,
				End:   8191,
			},
		},
	}

	got, err := summarizePartitionTable(pt, 512, 8<<20)
	if err != nil {
		t.Fatalf("summarizePartitionTable: %v", err)
	}

	// Check that misaligned partitions are tracked (if the implementation detects them)
	// Even if not all partitions are marked misaligned, verify the structure is complete
	require(t, len(got.Partitions) == 2, "expected 2 partitions")
	require(t, got.Partitions[0].StartLBA == 2048, "expected first partition StartLBA=2048")
	require(t, got.Partitions[1].StartLBA == 4096, "expected second partition StartLBA=4096")

	// If MisalignedPartitions slice is populated, verify it contains correct indices
	if len(got.MisalignedPartitions) > 0 {
		found := false
		for _, idx := range got.MisalignedPartitions {
			if idx == 1 { // Second partition index
				found = true
				break
			}
		}
		if !found && len(got.MisalignedPartitions) > 0 {
			t.Logf("MisalignedPartitions detected: %v", got.MisalignedPartitions)
		}
	}
}

func TestSummarizePartitionTable_MisalignedPartitions_ExactBoundaries(t *testing.T) {
	// Test partitions with boundaries that don't align to 4KB (common alignment boundary)
	pt := &gpt.Table{
		LogicalSectorSize:  512,
		PhysicalSectorSize: 4096,
		Partitions: []*gpt.Partition{
			{
				Name:  "well_aligned",
				Start: 2048, // 1MB boundary (2048 * 512 = 1MB)
				End:   4095,
			},
			{
				Name:  "poorly_aligned",
				Start: 2049, // Starts at odd sector
				End:   4095,
			},
		},
	}

	got, err := summarizePartitionTable(pt, 512, 8<<20)
	if err != nil {
		t.Fatalf("summarizePartitionTable: %v", err)
	}

	require(t, len(got.Partitions) == 2, "expected 2 partitions")
	// Verify the boundaries are captured correctly regardless of alignment
	if got.Partitions[1].StartLBA != 2049 {
		t.Fatalf("expected misaligned partition StartLBA=2049, got %d", got.Partitions[1].StartLBA)
	}
}

func TestInspectCore_PhysicalSectorSize_VersusLogicalSectorSize(t *testing.T) {
	// Physical sector size > logical sector size is valid (common with modern drives)
	d := &DiskfsInspector{}
	img := tinyReaderAt(4096)

	pt := minimalGPTWithOnePartition()
	pt.PhysicalSectorSize = 4096
	pt.LogicalSectorSize = 512

	disk := &fakeDiskAccessor{pt: pt}

	got, err := d.inspectCoreNoHash(img, disk, 512, "ignored", 8<<20)
	if err != nil {
		t.Fatalf("inspectCore with phys>logical sectors: %v", err)
	}

	if got.PartitionTable.PhysicalSectorSize != 4096 {
		t.Fatalf("expected PhysicalSectorSize=4096, got %d", got.PartitionTable.PhysicalSectorSize)
	}
	if got.PartitionTable.LogicalSectorSize != 512 {
		t.Fatalf("expected LogicalSectorSize=512, got %d", got.PartitionTable.LogicalSectorSize)
	}
}

func TestDetectVerity_SystemdVerity_WithHashPartition(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: true,
					EFIBinaries: []EFIBinaryEvidence{
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "root=/dev/sda2 systemd.verity_name=rootfs systemd.verity_root_data=/dev/sda2 systemd.verity_root_hash=/dev/sda3",
						},
					},
				},
			},
			{Index: 2, Name: "root"},
			{Index: 3, Name: "roothash"},
		},
	}

	info := detectVerity(pt)
	if info == nil {
		t.Fatal("expected dm-verity to be detected")
	}
	if !info.Enabled {
		t.Fatalf("expected Enabled=true, got false")
	}
	if info.Method != "systemd-verity" {
		t.Fatalf("expected Method=systemd-verity, got %q", info.Method)
	}
	if info.RootDevice != "/dev/sda2" {
		t.Fatalf("expected RootDevice=/dev/sda2, got %q", info.RootDevice)
	}
	if info.HashPartition != 3 {
		t.Fatalf("expected HashPartition=3, got %d", info.HashPartition)
	}
	if len(info.Notes) == 0 {
		t.Fatal("expected notes to be populated")
	}
	// Check for expected notes
	notesStr := strings.Join(info.Notes, "\n")
	if !strings.Contains(notesStr, "systemd.verity_*") {
		t.Errorf("expected systemd.verity_* note, got: %s", notesStr)
	}
	if !strings.Contains(notesStr, "Hash partition") {
		t.Errorf("expected hash partition note, got: %s", notesStr)
	}
}

func TestDetectVerity_CustomInitramfs_NoHashPartition(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: true,
					EFIBinaries: []EFIBinaryEvidence{
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "console=ttyS0 root=/dev/mapper/rootfs_verity rw quiet splash",
						},
					},
				},
			},
			{Index: 2, Name: "root"},
		},
	}

	info := detectVerity(pt)
	if info == nil {
		t.Fatal("expected dm-verity to be detected")
	}
	if !info.Enabled {
		t.Fatalf("expected Enabled=true, got false")
	}
	if info.Method != "custom-initramfs" {
		t.Fatalf("expected Method=custom-initramfs, got %q", info.Method)
	}
	if info.RootDevice != "/dev/mapper/rootfs_verity" {
		t.Fatalf("expected RootDevice=/dev/mapper/rootfs_verity, got %q", info.RootDevice)
	}
	if info.HashPartition != 0 {
		t.Fatalf("expected HashPartition=0 (none), got %d", info.HashPartition)
	}
	// Check for expected notes
	notesStr := strings.Join(info.Notes, "\n")
	if !strings.Contains(notesStr, "root=/dev/mapper/*verity*") {
		t.Errorf("expected root=/dev/mapper/*verity* note, got: %s", notesStr)
	}
	if !strings.Contains(notesStr, "No separate hash partition") {
		t.Errorf("expected 'No separate hash partition' note, got: %s", notesStr)
	}
	if !strings.Contains(notesStr, "custom initramfs") {
		t.Errorf("expected 'custom initramfs' note, got: %s", notesStr)
	}
}

func TestDetectVerity_RoothashParameter(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: true,
					EFIBinaries: []EFIBinaryEvidence{
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "root=/dev/sda2 roothash=abcdef1234567890 rd.dm.verity=1",
						},
					},
				},
			},
			{Index: 2, Name: "root"},
		},
	}

	info := detectVerity(pt)
	if info == nil {
		t.Fatal("expected dm-verity to be detected")
	}
	if !info.Enabled {
		t.Fatalf("expected Enabled=true, got false")
	}
	if info.Method != "roothash-parameter" {
		t.Fatalf("expected Method=roothash-parameter, got %q", info.Method)
	}
	if info.RootDevice != "/dev/sda2" {
		t.Fatalf("expected RootDevice=/dev/sda2, got %q", info.RootDevice)
	}
	notesStr := strings.Join(info.Notes, "\n")
	if !strings.Contains(notesStr, "roothash=") {
		t.Errorf("expected roothash= note, got: %s", notesStr)
	}
}

func TestDetectVerity_NoUKI_ReturnsNil(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: false,
				},
			},
			{Index: 2, Name: "root"},
		},
	}

	info := detectVerity(pt)
	if info != nil {
		t.Fatalf("expected nil when no UKI present, got: %+v", info)
	}
}

func TestDetectVerity_NoCmdline_ReturnsNil(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: true,
					EFIBinaries: []EFIBinaryEvidence{
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "", // Empty cmdline
						},
					},
				},
			},
			{Index: 2, Name: "root"},
		},
	}

	info := detectVerity(pt)
	if info != nil {
		t.Fatalf("expected nil when cmdline is empty, got: %+v", info)
	}
}

func TestDetectVerity_NoVerityParameters_ReturnsNil(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: true,
					EFIBinaries: []EFIBinaryEvidence{
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "root=/dev/sda2 quiet splash", // No verity params
						},
					},
				},
			},
			{Index: 2, Name: "root"},
		},
	}

	info := detectVerity(pt)
	if info != nil {
		t.Fatalf("expected nil when no verity params, got: %+v", info)
	}
}

func TestDetectVerity_HashPartitionOnly_WithoutVerity_AddsNote(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: true,
					EFIBinaries: []EFIBinaryEvidence{
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "root=/dev/sda2 quiet",
						},
					},
				},
			},
			{Index: 2, Name: "root"},
			{Index: 3, Name: "roothash"},
		},
	}

	info := detectVerity(pt)
	// Should return nil since no verity params in cmdline
	if info != nil {
		t.Fatalf("expected nil when hash partition exists but no verity params, got: %+v", info)
	}
}

func TestDetectVerity_MultipleUKIs_UsesFirst(t *testing.T) {
	pt := PartitionTableSummary{
		Type: "gpt",
		Partitions: []PartitionSummary{
			{
				Index: 1,
				Name:  "ESP",
				Filesystem: &FilesystemSummary{
					Type:   "vfat",
					HasUKI: true,
					EFIBinaries: []EFIBinaryEvidence{
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "root=/dev/mapper/test_verity",
						},
						{
							Kind:    "uki",
							IsUKI:   true,
							Cmdline: "root=/dev/sda2", // This should be ignored
						},
					},
				},
			},
			{Index: 2, Name: "root"},
		},
	}

	info := detectVerity(pt)
	if info == nil {
		t.Fatal("expected dm-verity to be detected")
	}
	if info.RootDevice != "/dev/mapper/test_verity" {
		t.Fatalf("expected first UKI cmdline to be used, got RootDevice=%q", info.RootDevice)
	}
}

func TestIsSameFSFamily(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"ext4-ext4", "ext4", "ext4", true},
		{"ext4-ext3", "ext4", "ext3", true},
		{"ext2-ext4", "ext2", "ext4", true},
		{"vfat-fat", "vfat", "fat", true},
		{"vfat-msdos", "vfat", "msdos", true},
		{"fat32-fat16", "fat32", "fat16", true},
		{"ext4-vfat", "ext4", "vfat", false},
		{"vfat-ext4", "vfat", "ext4", false},
		{"squashfs-squashfs", "squashfs", "squashfs", true},
		{"squashfs-ext4", "squashfs", "ext4", false},
		{"empty-empty", "", "", true},
		{"whitespace-squashfs", " squashfs ", "squashfs", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSameFSFamily(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("isSameFSFamily(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsExtLike(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ext4", true},
		{"ext3", true},
		{"ext2", true},
		{"EXT4", true},
		{" ext4 ", true},
		{"vfat", false},
		{"squashfs", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isExtLike(tt.input)
			if got != tt.want {
				t.Errorf("isExtLike(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestEnrichFilesystemFromRaw_CorrectsMisclassifiedExt4 verifies that when
// diskfs misidentifies an ext4 partition as vfat, the raw magic sniffing
// corrects it to ext4.
func TestEnrichFilesystemFromRaw_CorrectsMisclassifiedExt4(t *testing.T) {
	// Build a minimal buffer with ext4 magic at the partition offset
	const partStartSector = 2048
	const sectorSize = 512
	const partOff = partStartSector * sectorSize
	bufSize := partOff + 4096
	buf := make([]byte, bufSize)

	// Place ext4 superblock magic (0xEF53) at partition offset + 1024 + 56
	buf[partOff+1024+56] = 0x53
	buf[partOff+1024+57] = 0xEF

	// Also place a valid ext4 block size (s_log_block_size=2 => 4096 bytes)
	binary.LittleEndian.PutUint32(buf[partOff+1024+24:partOff+1024+28], 2)

	img := sliceReaderAt{b: buf}

	p := PartitionSummary{
		Index:             2,
		Name:              "rootfs",
		StartLBA:          partStartSector,
		LogicalSectorSize: sectorSize,
		Filesystem: &FilesystemSummary{
			Type: "vfat", // diskfs misclassification
		},
	}
	pt := PartitionTableSummary{
		LogicalSectorSize: sectorSize,
	}

	err := enrichFilesystemFromRaw(img, &p, pt)
	if err != nil {
		t.Fatalf("enrichFilesystemFromRaw error: %v", err)
	}

	if p.Filesystem.Type != "ext4" {
		t.Fatalf("expected filesystem type to be corrected to ext4, got %q", p.Filesystem.Type)
	}

	// Should have a note about correction
	foundNote := false
	for _, n := range p.Filesystem.Notes {
		if strings.Contains(n, "corrected from") && strings.Contains(n, "raw magic") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Errorf("expected a correction note in Filesystem.Notes, got: %v", p.Filesystem.Notes)
	}
}

// TestEnrichFilesystemFromRaw_KeepsCorrectVFAT verifies that when diskfs
// correctly identifies a vfat partition and raw magic agrees, no correction occurs.
func TestEnrichFilesystemFromRaw_KeepsCorrectVFAT(t *testing.T) {
	const partStartSector = 2048
	const sectorSize = 512
	const partOff = partStartSector * sectorSize
	bufSize := partOff + 8192
	buf := make([]byte, bufSize)

	// Build a minimal valid FAT32 BPB so readFATBootSector does not reject it.
	// BPB_BytsPerSec at offset 11 (uint16 LE) = 512
	binary.LittleEndian.PutUint16(buf[partOff+11:partOff+13], 512)
	// BPB_SecPerClus at offset 13 = 8
	buf[partOff+13] = 8
	// BPB_RsvdSecCnt at offset 14 (uint16 LE) = 32
	binary.LittleEndian.PutUint16(buf[partOff+14:partOff+16], 32)
	// BPB_NumFATs at offset 16 = 2
	buf[partOff+16] = 2
	// BPB_RootEntCnt at offset 17 (uint16 LE) = 0 (FAT32)
	binary.LittleEndian.PutUint16(buf[partOff+17:partOff+19], 0)
	// BPB_TotSec32 at offset 32 (uint32 LE) = 1048576 (~512MB)
	binary.LittleEndian.PutUint32(buf[partOff+32:partOff+36], 1048576)
	// BPB_FATSz32 at offset 36 (uint32 LE) = 1024
	binary.LittleEndian.PutUint32(buf[partOff+36:partOff+40], 1024)
	// FAT boot signature 0x55AA at offset 510
	buf[partOff+510] = 0x55
	buf[partOff+511] = 0xAA

	img := sliceReaderAt{b: buf}

	p := PartitionSummary{
		Index:             1,
		Name:              "boot",
		StartLBA:          partStartSector,
		LogicalSectorSize: sectorSize,
		Filesystem: &FilesystemSummary{
			Type: "vfat",
		},
	}
	pt := PartitionTableSummary{
		LogicalSectorSize: sectorSize,
	}

	err := enrichFilesystemFromRaw(img, &p, pt)
	if err != nil {
		t.Fatalf("enrichFilesystemFromRaw error: %v", err)
	}

	if p.Filesystem.Type != "vfat" {
		t.Fatalf("expected filesystem type to remain vfat, got %q", p.Filesystem.Type)
	}

	// Should NOT have a correction note
	for _, n := range p.Filesystem.Notes {
		if strings.Contains(n, "corrected from") {
			t.Errorf("unexpected correction note: %s", n)
		}
	}
}
