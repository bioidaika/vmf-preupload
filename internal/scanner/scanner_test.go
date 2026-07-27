package scanner

import (
	"testing"

	"github.com/bioidaika/vmf-preupload/pkg/api"
)

func TestPrimaryAudioKeepsMPEGCodecAndChannelsSeparate(t *testing.T) {
	for _, codec := range []string{"MP2", "MP3"} {
		t.Run(codec, func(t *testing.T) {
			got := primaryAudio([]api.Track{{Type: "Audio", Codec: codec, Channels: "2.0", Default: true}})
			want := codec + ".2.0"
			if got != want {
				t.Fatalf("primaryAudio()=%q want %q", got, want)
			}
		})
	}
}
