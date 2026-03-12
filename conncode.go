package main

import (
	"fmt"
	"strconv"
	"strings"
)

const defaultNestPort = 9090

// 256 short words — index = byte value (0-255), must match zipp-nest wordList
var wordList = [256]string{
	"ace", "age", "aid", "aim", "air", "ale", "ant", "arc",
	"arm", "art", "ash", "axe", "bay", "bed", "bee", "big",
	"bit", "box", "boy", "bud", "bug", "bun", "bus", "cab",
	"can", "cap", "car", "cat", "cow", "cup", "cut", "dad",
	"dam", "day", "den", "dew", "dig", "dim", "dip", "dog",
	"dot", "dry", "ear", "eat", "egg", "elf", "elk", "elm",
	"end", "era", "eye", "far", "fat", "fig", "fin", "fit",
	"fly", "fog", "fox", "fun", "fur", "gap", "gas", "gem",
	"gin", "gum", "gun", "gut", "gym", "ham", "hat", "hay",
	"hen", "hip", "hog", "hop", "hot", "hub", "hug", "hum",
	"ice", "ill", "imp", "ink", "inn", "ion", "ivy", "jab",
	"jam", "jar", "jaw", "jet", "jig", "job", "jot", "joy",
	"jug", "keg", "key", "kid", "kin", "kit", "lab", "lag",
	"lap", "law", "lay", "led", "leg", "lid", "lip", "lit",
	"log", "lot", "low", "lug", "mad", "map", "mat", "mob",
	"mop", "mud", "mug", "nap", "net", "nil", "nip", "nod",
	"nor", "nun", "oak", "oar", "odd", "oil", "old", "orb",
	"ore", "owl", "own", "pad", "pan", "paw", "pay", "pea",
	"peg", "pen", "pet", "pie", "pig", "pin", "pit", "pod",
	"pop", "pot", "pub", "pug", "pun", "pup", "ram", "ran",
	"rat", "raw", "ray", "red", "rid", "rig", "rim", "rip",
	"rob", "rod", "rot", "row", "rub", "rug", "rum", "run",
	"rut", "rye", "sad", "sap", "sat", "saw", "say", "sea",
	"set", "sew", "shy", "sin", "sip", "sir", "sit", "ski",
	"sky", "sly", "sob", "son", "sow", "spa", "spy", "sub",
	"sue", "sum", "sun", "tab", "tan", "tap", "tar", "tax",
	"tea", "tie", "tin", "tip", "toe", "ton", "top", "tow",
	"tug", "tun", "two", "urn", "van", "vat", "vet", "via",
	"vim", "vow", "wag", "war", "wax", "web", "wed", "wet",
	"wig", "win", "wit", "woe", "won", "wry", "yak", "yam",
	"yap", "yew", "zap", "zen", "zit", "cod", "cob", "cog",
	"cot", "cue", "cud", "dab", "dud", "dun", "fad", "foe",
}

// wordIndex is the reverse lookup: word → byte value
var wordIndex = func() map[string]int {
	m := make(map[string]int, 256)
	for i, w := range wordList {
		m[w] = i
	}
	return m
}()

// decodeNestCode decodes a 4-word code (e.g. "kin-hay-fig-big") back to "IP:port".
// If input already contains a dot it is treated as a full IP address (port appended if missing).
func decodeNestCode(code string) (string, error) {
	code = strings.TrimSpace(code)

	// full address passed directly
	if strings.Contains(code, ".") {
		if !strings.Contains(code, ":") {
			return code + fmt.Sprintf(":%d", defaultNestPort), nil
		}
		return code, nil
	}

	words := strings.Split(strings.ToLower(code), "-")
	if len(words) != 4 {
		return "", fmt.Errorf("expected 4 words like oak-fox-red-ice")
	}
	octets := make([]string, 4)
	for i, w := range words {
		idx, ok := wordIndex[w]
		if !ok {
			return "", fmt.Errorf("unknown word: %s", w)
		}
		octets[i] = strconv.Itoa(idx)
	}
	return strings.Join(octets, ".") + fmt.Sprintf(":%d", defaultNestPort), nil
}
