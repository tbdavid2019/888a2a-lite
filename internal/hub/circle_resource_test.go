package hub

import "testing"

func TestValidateCircleResourcePayload(t *testing.T) {
	docRes, err := ValidateCircleResourcePayload(
		"arch-notes",
		ResourceKindDoc,
		"",
		"",
		"# Architecture\nRead-only architecture doc referencing [888box](https://box.david888.com/skill.php).",
		"",
		"",
		"",
		0,
		"",
	)
	if err != nil {
		t.Fatalf("expected valid doc resource, got %v", err)
	}
	if docRes.Kind != ResourceKindDoc || docRes.Title != "Architecture" || docRes.ByteSize == 0 || len(docRes.ContentHash) != 64 {
		t.Fatalf("unexpected validated doc: %+v", docRes)
	}

	assetRes, err := ValidateCircleResourcePayload(
		"hero-banner",
		ResourceKindAsset,
		"Space Banner",
		"Homepage OG asset",
		"",
		"https://box.david888.com/storage/banner.webp",
		"image/webp",
		"",
		12840,
		"",
	)
	if err != nil {
		t.Fatalf("expected valid asset resource, got %v", err)
	}
	if assetRes.Kind != ResourceKindAsset || assetRes.Filename != "banner.webp" || assetRes.ByteSize != 0 || assetRes.ContentHash != "" {
		t.Fatalf("unexpected validated asset: %+v", assetRes)
	}
	assetWithHash, err := ValidateCircleResourcePayload(
		"hero-banner",
		ResourceKindAsset,
		"Space Banner",
		"",
		"",
		"https://box.david888.com/storage/banner.webp",
		"image/webp",
		"",
		12840,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	if err != nil || len(assetWithHash.ContentHash) != 64 {
		t.Fatalf("expected asset with supplied hash to preserve 64-hex hash: %+v err=%v", assetWithHash, err)
	}

	badCases := []struct {
		name      string
		kind      string
		content   string
		url       string
		mediaType string
		hash      string
		size      int64
	}{
		{name: "asset http", kind: ResourceKindAsset, url: "http://box.david888.com/a.png", mediaType: "image/png"},
		{name: "asset userinfo", kind: ResourceKindAsset, url: "https://user:pass@box.david888.com/a.png", mediaType: "image/png"},
		{name: "asset fragment", kind: ResourceKindAsset, url: "https://box.david888.com/a.png#x", mediaType: "image/png"},
		{name: "asset unsupported mime", kind: ResourceKindAsset, url: "https://box.david888.com/a.html", mediaType: "text/html"},
		{name: "asset bad hash", kind: ResourceKindAsset, url: "https://box.david888.com/a.png", mediaType: "image/png", hash: "not-sha256"},
		{name: "asset negative size", kind: ResourceKindAsset, url: "https://box.david888.com/a.png", mediaType: "image/png", size: -1},
		{name: "doc raw html", kind: ResourceKindDoc, content: "# Bad\n<script>alert(1)</script>"},
		{name: "doc with url", kind: ResourceKindDoc, content: "# Hi", url: "https://box.david888.com/a.png"},
	}
	for _, tc := range badCases {
		if _, err := ValidateCircleResourcePayload("valid-slug", tc.kind, "", "", tc.content, tc.url, tc.mediaType, "", tc.size, tc.hash); err == nil {
			t.Fatalf("%s: expected error but succeeded", tc.name)
		}
	}
	if _, err := ValidateCircleResourcePayload("proposals", ResourceKindDoc, "", "", "# Reserved", "", "", "", 0, ""); err == nil {
		t.Fatal("expected reserved slug 'proposals' to fail validation")
	}
}
