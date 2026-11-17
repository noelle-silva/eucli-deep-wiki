package main

import "testing"

func TestRepositoryURLsSplitsArguments(t *testing.T) {
	t.Parallel()

	urls := repositoryURLs([]string{
		"https://github.com/example/one.git\nhttps://github.com/example/two.git",
		" https://github.com/example/three.git ",
	})

	want := []string{
		"https://github.com/example/one.git",
		"https://github.com/example/two.git",
		"https://github.com/example/three.git",
	}
	if len(urls) != len(want) {
		t.Fatalf("len(urls) = %d, want %d: %#v", len(urls), len(want), urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}
