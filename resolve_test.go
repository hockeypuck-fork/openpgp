/*
   Hockeypuck - OpenPGP key server
   Copyright (C) 2012-2014  Casey Marshall

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, version 3.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package openpgp

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"time"

	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
	gc "gopkg.in/check.v1"

	"github.com/schmorrison/testing"
)

type ResolveSuite struct{}

var _ = gc.Suite(&ResolveSuite{})

func (s *ResolveSuite) TestBadSelfSigUid(c *gc.C) {
	f := testing.MustInput("badselfsig.asc")
	var keys []*ReadKeyResult
	for kr := range ReadKeys(f) {
		keys = append(keys, kr)
	}
	c.Assert(keys, gc.HasLen, 1)
	c.Assert(keys[0].Error, gc.NotNil)
}

func (s *ResolveSuite) TestDupSigSksDigest(c *gc.C) {
	f := testing.MustInput("252B8B37.dupsig.asc")
	defer f.Close()
	block, err := armor.Decode(f)
	c.Assert(err, gc.IsNil)
	r := packet.NewOpaqueReader(block.Body)
	var packets []*packet.OpaquePacket
	for {
		if op, err := r.Next(); err != nil {
			break
		} else {
			packets = append(packets, op)
			c.Log("raw:", op)
		}
	}
	sksDigest := sksDigestOpaque(packets, md5.New())
	c.Assert(sksDigest, gc.Equals, "6d57b48c83d6322076d634059bb3b94b")
}

func (s *ResolveSuite) TestRoundTripSksDigest(c *gc.C) {
	f := testing.MustInput("252B8B37.dupsig.asc")
	defer f.Close()
	block, err := armor.Decode(f)
	c.Assert(err, gc.IsNil)

	var key *PrimaryKey
	for keyRead := range readKeys(block.Body) {
		c.Assert(keyRead.Error, gc.IsNil)
		key = keyRead.PrimaryKey
	}

	var packets []*packet.OpaquePacket
	for _, node := range key.contents() {
		packet := node.packet()
		for i := 0; i <= packet.Count; i++ {
			op, err := newOpaquePacket(packet.Packet)
			c.Assert(err, gc.IsNil)
			packets = append(packets, op)
		}
	}

	sksDigest := sksDigestOpaque(packets, md5.New())
	c.Assert(sksDigest, gc.Equals, "6d57b48c83d6322076d634059bb3b94b")
}

func patchNow(t time.Time) func() {
	now = func() time.Time {
		return t
	}
	return func() {
		now = time.Now
	}
}

func (s *ResolveSuite) TestUserIDSelfSigs(c *gc.C) {
	defer patchNow(time.Date(2014, time.January, 1, 0, 0, 0, 0, time.UTC))()

	key := MustInputAscKey("lp1195901.asc")
	err := DropDuplicates(key)
	c.Assert(err, gc.IsNil)
	Sort(key)
	// Primary UID
	c.Assert(key.UserIDs[0].Keywords, gc.Equals, "Phil Pennock <phil.pennock@spodhuis.org>")
	for _, uid := range key.UserIDs {
		if uid.Keywords == "pdp@spodhuis.demon.nl" {
			ss := uid.SelfSigs(key)
			c.Assert(ss.Revocations, gc.HasLen, 1)
		}
	}

	key = MustInputAscKey("lp1195901_2.asc")
	err = DropDuplicates(key)
	c.Assert(err, gc.IsNil)
	Sort(key)
	c.Assert(key.UserIDs[0].Keywords, gc.Equals, "Phil Pennock <phil.pennock@globnix.org>")
}

func (s *ResolveSuite) TestSortUserIDs(c *gc.C) {
	defer patchNow(time.Date(2014, time.January, 1, 0, 0, 0, 0, time.UTC))()

	key := MustInputAscKey("lp1195901.asc")
	err := DropDuplicates(key)
	c.Assert(err, gc.IsNil)
	Sort(key)
	expect := []string{
		"Phil Pennock <phil.pennock@spodhuis.org>",
		"Phil Pennock <pdp@exim.org>",
		"Phil Pennock <phil.pennock@globnix.org>",
		"Phil Pennock <pdp@spodhuis.org>",
		"Phil Pennock <pdp@spodhuis.demon.nl>"}
	for i := range key.UserIDs {
		c.Assert(key.UserIDs[i].Keywords, gc.Equals, expect[i])
	}
}

func (s *ResolveSuite) TestKeyExpiration(c *gc.C) {
	defer patchNow(time.Date(2013, time.January, 1, 0, 0, 0, 0, time.UTC))()

	key := MustInputAscKey("lp1195901.asc")
	err := DropDuplicates(key)
	c.Assert(err, gc.IsNil)
	Sort(key)

	c.Assert(key.SubKeys, gc.HasLen, 7)
	c.Assert(key.SubKeys[0].UUID, gc.Equals, "6c949d8098859e7816e6b33d54d50118a1b8dfc9")
	c.Assert(key.SubKeys[1].UUID, gc.Equals, "3745e9590264de539613d833ad83b9366e3d6be3")
}

// TestUnsuppIgnored tests parsing key material containing
// packets which are not normally part of an exported public key --
// trust packets, in this case.
func (s *ResolveSuite) TestUnsuppIgnored(c *gc.C) {
	f := testing.MustInput("snowcrash.gpg")
	var key *PrimaryKey
	for keyRead := range ReadKeys(f) {
		c.Assert(keyRead.Error, gc.IsNil)
		key = keyRead.PrimaryKey
		break
	}
	c.Assert(key, gc.NotNil)
	for _, node := range key.contents() {
		switch p := node.(type) {
		case *PrimaryKey:
			c.Assert(p.Others, gc.HasLen, 0)
		case *SubKey:
			c.Assert(p.Others, gc.HasLen, 0)
		case *UserID:
			c.Assert(p.Others, gc.HasLen, 0)
		case *UserAttribute:
			c.Assert(p.Others, gc.HasLen, 0)
		}
	}
}

func (s *ResolveSuite) TestMissingUidFk(c *gc.C) {
	key := MustInputAscKey("d7346e26.asc")
	c.Log(key)
}

func (s *ResolveSuite) TestV3NoUidSig(c *gc.C) {
	key := MustInputAscKey("0xd46b7c827be290fe4d1f9291b1ebc61a.asc")
	c.Assert(key.RKeyID, gc.Equals, "93228d3b46fd0670")
	f := testing.MustInput("0xd46b7c827be290fe4d1f9291b1ebc61a.asc")
	defer f.Close()
	block, err := armor.Decode(f)
	c.Assert(err, gc.IsNil)
	var kr *OpaqueKeyring
	for opkr := range ReadOpaqueKeyrings(block.Body) {
		kr = opkr
	}
	sort.Sort(opaquePacketSlice(kr.Packets))
	h := md5.New()
	for _, opkt := range kr.Packets {
		binary.Write(h, binary.BigEndian, int32(opkt.Tag))
		binary.Write(h, binary.BigEndian, int32(len(opkt.Contents)))
		h.Write(opkt.Contents)
	}
	md5 := hex.EncodeToString(h.Sum(nil))
	c.Assert("0005127a8b7da8c32998d7e81dc92540", gc.Equals, md5)
}

func (s *ResolveSuite) TestMergeAddSig(c *gc.C) {
	unsignedKeys := MustInputAscKeys("alice_unsigned.asc")
	c.Assert(unsignedKeys, gc.HasLen, 1)
	c.Assert(unsignedKeys[0], gc.NotNil)
	signedKeys := MustInputAscKeys("alice_signed.asc")
	c.Assert(signedKeys, gc.HasLen, 1)
	c.Assert(signedKeys[0], gc.NotNil)

	c.Assert(unsignedKeys[0].UserIDs, gc.HasLen, 1)
	c.Assert(signedKeys[0].UserIDs, gc.HasLen, 1)

	c.Assert(unsignedKeys[0].UserIDs[0].Signatures, gc.HasLen, 1)
	c.Assert(signedKeys[0].UserIDs[0].Signatures, gc.HasLen, 2)

	hasExpectedSig := func(key *PrimaryKey) bool {
		for _, node := range key.contents() {
			sig, ok := node.(*Signature)
			if ok {
				c.Logf("sig from %s", sig.RIssuerKeyID)
				if sig.RIssuerKeyID == "5bf04676d10aea26" {
					return true
				}
			}
		}
		return false
	}
	c.Assert(hasExpectedSig(unsignedKeys[0]), gc.Equals, false)
	c.Assert(hasExpectedSig(signedKeys[0]), gc.Equals, true)
	err := Merge(unsignedKeys[0], signedKeys[0])
	c.Assert(err, gc.IsNil)
	c.Assert(hasExpectedSig(unsignedKeys[0]), gc.Equals, true)
}
