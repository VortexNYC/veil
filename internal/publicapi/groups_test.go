package publicapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/VortexNYC/veil/internal/protocol"
)

// Groups are the team model over HTTP: owner creates, lists, and manages
// membership; a grant to a group name is the shared vault. Member humans
// and agents get 403 — the same gate as grants.

func TestGroupsRoundTrip(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)

	// Create.
	code, raw := doJSON(t, srv, http.MethodPost, "/v1/groups", "human", CreateGroupRequest{Name: "eng"})
	if code != http.StatusOK {
		t.Fatalf("create group: %d %s", code, raw)
	}
	var g protocol.Group
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.Name != "eng" || g.OrgID != protocol.LocalOrgID || g.ID == "" {
		t.Fatalf("group: %+v", g)
	}

	// List.
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/groups", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list groups: %d %s", code, raw)
	}
	var gl GroupsResponse
	if err := json.Unmarshal(raw, &gl); err != nil {
		t.Fatal(err)
	}
	if len(gl.Groups) != 1 || gl.Groups[0].Name != "eng" {
		t.Fatalf("groups: %+v", gl.Groups)
	}

	// An agent member needs a live agent row — create one via the API.
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/agents", "human", CreateAgentRequest{Name: "bot"})
	if code != http.StatusOK {
		t.Fatalf("create agent: %d %s", code, raw)
	}
	var agent protocol.Principal
	if err := json.Unmarshal(raw, &agent); err != nil {
		t.Fatal(err)
	}

	// Add the agent and the owner human as members.
	for _, m := range []AddGroupMemberRequest{
		{MemberKind: "agent", MemberID: agent.ID},
		{MemberKind: "human", MemberID: "self"},
	} {
		code, raw = doJSON(t, srv, http.MethodPost, "/v1/groups/eng/members", "human", m)
		if code != http.StatusOK {
			t.Fatalf("add member %s: %d %s", m.MemberID, code, raw)
		}
	}

	code, raw = doJSON(t, srv, http.MethodGet, "/v1/groups/eng/members", "human", nil)
	if code != http.StatusOK {
		t.Fatalf("list members: %d %s", code, raw)
	}
	var ml GroupMembersResponse
	if err := json.Unmarshal(raw, &ml); err != nil {
		t.Fatal(err)
	}
	if len(ml.Members) != 2 {
		t.Fatalf("members: %+v", ml.Members)
	}

	// Remove the agent — one member remains.
	code, raw = doJSON(t, srv, http.MethodDelete, "/v1/groups/eng/members/agent/"+agent.ID, "human", nil)
	if code != http.StatusOK {
		t.Fatalf("remove member: %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodGet, "/v1/groups/eng/members", "human", nil)
	if err := json.Unmarshal(raw, &ml); err != nil {
		t.Fatal(err)
	}
	if len(ml.Members) != 1 || ml.Members[0].MemberID != "self" {
		t.Fatalf("members after remove: %+v", ml.Members)
	}
}

func TestGroupsOwnerGated(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)
	if code, _ := doJSON(t, srv, http.MethodPost, "/v1/groups", "human", CreateGroupRequest{Name: "eng"}); code != http.StatusOK {
		t.Fatal("seed group")
	}
	for _, tok := range []string{"", "member", "agent-x", "otherorg"} {
		if code, _ := doJSON(t, srv, http.MethodGet, "/v1/groups", tok, nil); code == http.StatusOK {
			t.Fatalf("list groups as %q should not be 200", tok)
		}
		if code, _ := doJSON(t, srv, http.MethodPost, "/v1/groups", tok, CreateGroupRequest{Name: "nope"}); code == http.StatusOK {
			t.Fatalf("create group as %q should not be 200", tok)
		}
		if code, _ := doJSON(t, srv, http.MethodGet, "/v1/groups/eng/members", tok, nil); code == http.StatusOK {
			t.Fatalf("list members as %q should not be 200", tok)
		}
		if code, _ := doJSON(t, srv, http.MethodPost, "/v1/groups/eng/members", tok,
			AddGroupMemberRequest{MemberKind: "human", MemberID: "self"}); code == http.StatusOK {
			t.Fatalf("add member as %q should not be 200", tok)
		}
		if code, _ := doJSON(t, srv, http.MethodDelete, "/v1/groups/eng/members/human/self", tok, nil); code == http.StatusOK {
			t.Fatalf("remove member as %q should not be 200", tok)
		}
	}
}

func TestGrantGroupSubjectOverHTTP(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)

	code, raw := doJSON(t, srv, http.MethodPost, "/v1/items", "human", CreateItemRequest{Name: "github", Secret: secret})
	if code != http.StatusOK {
		t.Fatalf("create item: %d %s", code, raw)
	}
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/groups", "human", CreateGroupRequest{Name: "eng"})
	if code != http.StatusOK {
		t.Fatalf("create group: %d %s", code, raw)
	}

	// A group grant is one row — the shared vault shape.
	code, raw = doJSON(t, srv, http.MethodPost, "/v1/grants", "human",
		CreateGrantRequest{Group: "eng", Item: "github", Level: "level2"})
	if code != http.StatusOK {
		t.Fatalf("group grant: %d %s", code, raw)
	}
	var g GrantView
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.SubjectKind != "group" || g.ItemID != "github" {
		t.Fatalf("grant: %+v", g)
	}

	// XOR across all three subjects.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/grants", "human",
		CreateGrantRequest{Group: "eng", Agent: "claude", Item: "github", Level: "level2"})
	if code != http.StatusBadRequest {
		t.Fatalf("agent+group XOR: %d", code)
	}
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/grants", "human",
		CreateGrantRequest{Group: "eng", Human: "self", Item: "github", Level: "level2"})
	if code != http.StatusBadRequest {
		t.Fatalf("human+group XOR: %d", code)
	}

	// Unknown group name fails.
	code, _ = doJSON(t, srv, http.MethodPost, "/v1/grants", "human",
		CreateGrantRequest{Group: "nope", Item: "github", Level: "level2"})
	if code != http.StatusOK {
		return
	}
	t.Fatal("grant to unknown group should fail")
}
