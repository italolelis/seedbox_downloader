package transfer

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// files builds a file list from relative paths, which is all LocalName reads.
func files(paths ...string) []*File {
	out := make([]*File, 0, len(paths))
	for i, p := range paths {
		out = append(out, &File{ID: int64(i + 1), Path: filepath.FromSlash(p), Size: 1})
	}

	return out
}

// The four rows reported in #9. Each is a real transfer from a live deployment
// where the advertised name and the on-disk path disagreed. The point of these
// cases is that the name comes from the file paths, so a collision suffix or an
// unrelated seedbox rename is carried rather than lost.
func TestLocalName_TheFourReportedCases(t *testing.T) {
	tests := []struct {
		name         string
		transferName string
		files        []*File
		want         string
	}{
		{
			name:         "folder transfer that already imported correctly",
			transferName: "Jerry.and.Marge.Go.Large.2022.1080p.WEBRip.DD5.1-FGT",
			files: files(
				"Jerry.and.Marge.Go.Large.2022.1080p.WEBRip.DD5.1-FGT/movie.mkv",
				"Jerry.and.Marge.Go.Large.2022.1080p.WEBRip.DD5.1-FGT/sample.mkv",
			),
			want: "Jerry.and.Marge.Go.Large.2022.1080p.WEBRip.DD5.1-FGT",
		},
		{
			name:         "single file diverging only by its extension",
			transferName: "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
			files:        files("Silo.S03E07.1080p.DUAL-SiGLA.mkv"),
			want:         "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
		},
		{
			name:         "single file carrying a collision suffix the transfer name cannot have",
			transferName: "Silo S03E08 HiggsBoson .exe",
			files:        files("Silo S03E08 HiggsBoson  ojqRfI77.exe"),
			want:         "Silo S03E08 HiggsBoson  ojqRfI77.exe",
		},
		{
			name:         "single file whose seedbox rename shares nothing with the transfer name",
			transferName: "Minions.2015.2160p.H265.MP4-BTM",
			files:        files("Minions.2015.2160p.HDR10+[Ben The Men] ojqYDnu2.mp4"),
			want:         "Minions.2015.2160p.HDR10+[Ben The Men] ojqYDnu2.mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := &Transfer{Name: tt.transferName, Files: tt.files}

			got, derived := tr.LocalName()

			assert.True(t, derived, "name should be derived from the file paths, not fall back")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLocalName(t *testing.T) {
	tests := []struct {
		name         string
		transferName string
		files        []*File
		want         string
		wantDerived  bool
	}{
		{
			name:         "single file keeps its extension and gets no enclosing folder",
			transferName: "irrelevant",
			files:        files("movie.mkv"),
			want:         "movie.mkv",
			wantDerived:  true,
		},
		{
			name:         "single file with no extension",
			transferName: "irrelevant",
			files:        files("movie"),
			want:         "movie",
			wantDerived:  true,
		},
		{
			name:         "single file with multiple dots",
			transferName: "irrelevant",
			files:        files("the.movie.2024.1080p.mkv"),
			want:         "the.movie.2024.1080p.mkv",
			wantDerived:  true,
		},
		{
			name:         "single file with spaces and bracketed release tags",
			transferName: "irrelevant",
			files:        files("The Movie 2024 [Group] [1080p].mkv"),
			want:         "The Movie 2024 [Group] [1080p].mkv",
			wantDerived:  true,
		},
		{
			name:         "folder transfer with one file still resolves to the folder",
			transferName: "irrelevant",
			files:        files("Season.01/episode.mkv"),
			want:         "Season.01",
			wantDerived:  true,
		},
		{
			name:         "folder transfer with nested folders resolves to the outermost",
			transferName: "irrelevant",
			files: files(
				"Show.S01/Season 1/episode1.mkv",
				"Show.S01/Season 1/episode2.mkv",
				"Show.S01/extras/behind.mkv",
			),
			want:        "Show.S01",
			wantDerived: true,
		},
		{
			name:         "the transfer name is ignored even when it looks more correct",
			transferName: "The.Nicer.Looking.Name",
			files:        files("ugly_scene_name_xyz/episode.mkv"),
			want:         "ugly_scene_name_xyz",
			wantDerived:  true,
		},
		{
			name:         "empty file list falls back to the transfer name",
			transferName: "In.Progress.Transfer",
			files:        nil,
			want:         "In.Progress.Transfer",
			wantDerived:  false,
		},
		{
			name:         "files sharing no common root fall back to the transfer name",
			transferName: "Weird.Shape",
			files:        files("one/episode.mkv", "two/episode.mkv"),
			want:         "Weird.Shape",
			wantDerived:  false,
		},
		{
			name:         "multiple bare files at the root fall back to the transfer name",
			transferName: "Also.Weird",
			files:        files("a.mkv", "b.mkv"),
			want:         "Also.Weird",
			wantDerived:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := &Transfer{Name: tt.transferName, Files: tt.files}

			got, derived := tr.LocalName()

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantDerived, derived)
		})
	}
}

// Until single-file transfers stop being written into a wrapper folder, the
// seedbox client still produces "movie/movie.mkv". LocalName resolves that to
// the wrapper, which is genuinely what is on disk -- so it stays correct across
// that change rather than depending on it having landed.
func TestLocalName_ResolvesTheLegacyWrapperLayout(t *testing.T) {
	t.Parallel()

	tr := &Transfer{
		Name:  "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
		Files: files("Silo.S03E07.1080p.DUAL-SiGLA/Silo.S03E07.1080p.DUAL-SiGLA.mkv"),
	}

	got, derived := tr.LocalName()

	assert.True(t, derived)
	assert.Equal(t, "Silo.S03E07.1080p.DUAL-SiGLA", got,
		"the wrapper folder is what exists on disk today, so that is the honest answer")
}
