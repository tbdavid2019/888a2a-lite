package circle

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	ModeSingle      = "single"
	ModeMulti       = "multi"
	PublicID        = "public"
	maxAliasLength  = 64
	maxSharedKeyLen = 4096
)

type Alias struct {
	Name   string
	Secret string
}

type Resolver struct {
	mode          string
	allowDynamic  bool
	derivationKey []byte
	aliases       []Alias
}

type Identity struct {
	ID         string
	Alias      string
	KeyDigest  string
	KeyVersion string
}

func NewResolver(mode, sharedKeys, derivationSecret string, allowDynamic bool) (Resolver, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = ModeSingle
	}
	if mode != ModeSingle && mode != ModeMulti {
		return Resolver{}, fmt.Errorf("circle mode must be %q or %q", ModeSingle, ModeMulti)
	}
	derivationSecret = strings.TrimSpace(derivationSecret)
	if mode == ModeMulti && derivationSecret == "" {
		return Resolver{}, errors.New("circle derivation secret is required in multi mode")
	}
	aliases, err := ParseAliases(sharedKeys)
	if err != nil {
		return Resolver{}, err
	}
	return Resolver{
		mode:          mode,
		allowDynamic:  allowDynamic,
		derivationKey: []byte(derivationSecret),
		aliases:       aliases,
	}, nil
}

func ParseAliases(value string) ([]Alias, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	seenNames := make(map[string]struct{})
	seenSecrets := make(map[string]struct{})
	aliases := make([]Alias, 0)
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, errors.New("shared key list contains an empty entry")
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("shared key entries must use alias:secret format")
		}
		name, secret := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if !validAlias(name) {
			return nil, fmt.Errorf("shared key alias must be 1-%d ASCII letters, numbers, '_' or '-'", maxAliasLength)
		}
		if secret == "" || len(secret) > maxSharedKeyLen {
			return nil, fmt.Errorf("shared key secret must be 1-%d bytes", maxSharedKeyLen)
		}
		if _, exists := seenNames[name]; exists {
			return nil, fmt.Errorf("shared key alias %q is duplicated", name)
		}
		if _, exists := seenSecrets[secret]; exists {
			return nil, errors.New("the same shared key secret cannot map to multiple aliases")
		}
		seenNames[name] = struct{}{}
		seenSecrets[secret] = struct{}{}
		aliases = append(aliases, Alias{Name: name, Secret: secret})
	}
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Name < aliases[j].Name })
	return aliases, nil
}

func (resolver Resolver) Mode() string { return resolver.mode }

func (resolver Resolver) Resolve(sharedKey string) (Identity, error) {
	if resolver.mode == ModeSingle {
		return Identity{ID: PublicID, KeyVersion: "0"}, nil
	}
	sharedKey = strings.TrimSpace(sharedKey)
	if sharedKey == "" {
		return Identity{ID: PublicID, KeyVersion: "0"}, nil
	}
	for _, alias := range resolver.aliases {
		if secureEqual(alias.Secret, sharedKey) {
			return Identity{
				ID:         resolver.aliasID(alias.Name),
				Alias:      alias.Name,
				KeyDigest:  resolver.digest("alias:" + alias.Name),
				KeyVersion: "1",
			}, nil
		}
	}
	if !resolver.allowDynamic {
		return Identity{}, errors.New("shared key is not allowed")
	}
	return Identity{
		ID:         "circle-" + resolver.digest("key:" + sharedKey)[:32],
		KeyDigest:  resolver.digest("key:" + sharedKey),
		KeyVersion: "1",
	}, nil
}

func (resolver Resolver) aliasID(alias string) string {
	return "circle-" + resolver.digest("alias:" + alias)[:32]
}

func (resolver Resolver) digest(value string) string {
	mac := hmac.New(sha256.New, resolver.derivationKey)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func validAlias(value string) bool {
	if value == "" || len(value) > maxAliasLength {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
