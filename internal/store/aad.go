package store

import "github.com/VortexNYC/veil/internal/protocol"

// AAD formats for epoch-1 ("VEIL1"-marked) ciphertexts. The leading domain tag
// keeps an item blob from ever satisfying an owner-wrap context or vice versa,
// even if the id bytes coincidentally align. Rows predating the marker open
// nil-AAD via crypto.OpenEpoch.
func itemAAD(orgID, itemID string) []byte {
	return []byte("veil/item/v1\x00" + orgID + "\x00" + itemID)
}

func ownerWrapAAD(orgID string, o protocol.Owner) []byte {
	return []byte("veil/ownerkey/v1\x00" + orgID + "\x00" + string(o.Kind) + "\x00" + o.ID)
}
