package main

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// wordlist holds 256 short, distinct, easily-read-aloud words. A
// fingerprint renders as 6 of these (48 bits of the SHA-256) —
// "correct horse battery staple" style — for the TOFU ceremony. The raw
// hex is always shown alongside; the words are a human checksum, not a
// replacement.
var wordlist = []string{
	"amber", "anchor", "antler", "apricot", "arcade", "arctic", "ashen", "atlas",
	"aurora", "autumn", "badger", "bamboo", "banjo", "basalt", "beacon", "birch",
	"bison", "blizzard", "bolt", "bonfire", "boulder", "breeze", "brisk", "brook",
	"brush", "cactus", "cadence", "canyon", "carbon", "cascade", "cedar", "chasm",
	"citrus", "cliff", "cobalt", "comet", "copper", "coral", "cove", "crater",
	"cricket", "crimson", "crow", "crystal", "cypress", "dahlia", "dawn", "delta",
	"dune", "eagle", "ebony", "eclipse", "ember", "falcon", "fern", "fjord",
	"flint", "flurry", "forest", "fossil", "foxglove", "frost", "galaxy", "garnet",
	"geyser", "glacier", "glimmer", "granite", "grove", "gull", "harbor", "hawk",
	"hazel", "heather", "heron", "hollow", "horizon", "husky", "igloo", "indigo",
	"inlet", "iron", "island", "ivory", "jaguar", "jasper", "juniper", "kelp",
	"kestrel", "lagoon", "lantern", "larch", "lava", "lichen", "lilac", "linen",
	"lotus", "lumen", "magnet", "mango", "maple", "marble", "marsh", "meadow",
	"mercury", "mesa", "midnight", "mirage", "mist", "monsoon", "moss", "moth",
	"mulberry", "nebula", "nectar", "needle", "nickel", "north", "nova", "oasis",
	"obsidian", "ocean", "onyx", "orchid", "otter", "owl", "oxide", "pampas",
	"panda", "pebble", "peridot", "petal", "pine", "pioneer", "plover", "prairie",
	"prism", "puma", "quartz", "quill", "radar", "rain", "raven", "reef",
	"ridge", "river", "robin", "rocket", "rune", "sable", "saffron", "sand",
	"sapphire", "savanna", "sequoia", "signal", "silver", "slate", "solar", "sonar",
	"spark", "spruce", "star", "stone", "storm", "summit", "sunset", "taiga",
	"talon", "tango", "teal", "tempest", "thunder", "tidal", "timber", "topaz",
	"torch", "tundra", "turbo", "twilight", "umbra", "valley", "vapor", "velvet",
	"vernal", "vortex", "walnut", "wasabi", "willow", "wisdom", "wren", "yarrow",
	"yew", "yonder", "zephyr", "zinc", "zinnia", "acorn", "alder", "anemone", "aster",
	"avalanche", "azalea", "bandit", "barley", "basil", "bayou", "beetle", "boreal",
	"bramble", "butte", "caldera", "camellia", "caribou", "cavern", "cherry", "clover",
	"condor", "coyote", "crag", "cumin", "daisy", "dorsal", "drift", "egret",
	"elm", "ermine", "fennec", "finch", "floe", "flora", "gale", "ginkgo",
	"glade", "harp", "helios", "ibis", "iceberg", "inkcap", "iris", "jackal",
	"jasmine", "krypton", "lark", "lupine", "lunar", "lyre", "magma", "mantis",
	"marigold", "mint", "monarch", "nimbus", "nomad", "oak", "opal",
}

func init() {
	// Guard the ceremony: the encoding indexes wordlist by byte, so it
	// must be exactly 256 unique entries. Fail loud at startup, not
	// mid-handshake.
	if len(wordlist) != 256 {
		panic(fmt.Sprintf("wiredrop: wordlist has %d entries, want 256", len(wordlist)))
	}
	seen := map[string]bool{}
	for _, w := range wordlist {
		if seen[w] {
			panic("wiredrop: duplicate word in wordlist: " + w)
		}
		seen[w] = true
	}
}

// fingerprintWords renders the first 48 bits of a hex fingerprint as 6
// human words. Deterministic: the same fingerprint always reads the same.
func fingerprintWords(fpHex string) string {
	raw, err := hex.DecodeString(fpHex)
	if err != nil || len(raw) < 6 {
		return "(unreadable fingerprint)"
	}
	words := make([]string, 6)
	for i := 0; i < 6; i++ {
		words[i] = wordlist[raw[i]]
	}
	return strings.Join(words, " ")
}
