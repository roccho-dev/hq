package core_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"hq/internal/core"
)

func TestAcceptedHistoryProjectionIsDeterministicAndWorldFirst(t *testing.T) {
	command := core.CommandDefinition{
		Kind: core.CommandInputKind, CommandID: "demo", CommandVersion: "1", Name: "demo",
		Instruction: map[string]any{"version":"instruction.v1","op":"run","target":"demo","payload":map[string]any{}},
		Fields: []core.CommandField{{Name:"value",Type:"string",Required:true,HistoryPolicy:core.HistoryPolicySearch,Examples:[]string{"alpha"},Bind:"payload.value"}},
	}
	world := &core.JsonlWorld{Identity:&core.WorldDefinition{Kind:core.WorldDefinitionKind,WorldID:"history.test"},Commands:[]core.CommandDefinition{command}}
	if err := world.RecomputeDigest(); err != nil { t.Fatal(err) }
	base, err := core.PrepareWorldRecall(world, func(identity core.WorldRecallCandidateIdentity)(string,error){ return core.CanonicalDigest(identity) })
	if err != nil { t.Fatal(err) }
	worldRef, _ := world.SelectedRef(); commandRef, _ := world.CommandRef("demo")
	record := func(id, created string) core.AcceptedHistoryRecord {
		instructionDigest, _ := core.CanonicalDigest(map[string]any{"id":id,"created_at":created,"value":"alpha"})
		input := core.AcceptedInput{Kind:core.AcceptedInputKind,World:worldRef,Command:commandRef,Fields:[]core.AcceptedInputField{{Name:"value",Type:"string",Value:"alpha"}},SuppliedFieldCount:1,RecallComplete:true,RenderContract:core.CommandObjectMaterializerVersion}
		if err := input.BindInstructionDigest(instructionDigest); err != nil { t.Fatal(err) }
		at, _ := time.Parse(time.RFC3339Nano, created)
		return core.AcceptedHistoryRecord{AcceptedID:id,AcceptedAt:at,Provenance:core.CompileProvenance{Kind:core.CompileProvenanceKind,InputKind:core.CommandInputKind,World:worldRef,Command:&commandRef,InstructionDigest:instructionDigest},Input:input}
	}
	rows := []core.AcceptedHistoryRecord{record("ins-1","2026-07-15T00:00:00Z"),record("ins-2","2026-07-16T00:00:00Z")}
	first, report := core.AttachAcceptedHistory(world,base,rows,func(identity core.WorldRecallCandidateIdentity)(string,error){ return core.CanonicalDigest(identity) })
	if report.Fatal || first.ID()==base.ID() { t.Fatalf("report=%#v index=%q",report,first.ID()) }
	recent := first.Recall(core.WorldRecallQuery{Scope:core.WorldRecallObjectQuery,RecentOnly:true})
	if len(recent)!=1 || recent[0].Rank.HistoryFrequency!=2 || recent[0].Materialization!="@demo\nvalue=alpha" { t.Fatalf("recent=%#v",recent) }
	values := first.Recall(core.WorldRecallQuery{Scope:core.WorldRecallFieldValue,CommandName:"demo",FieldName:"value",Text:"alpha"})
	if len(values)!=2 || values[0].Rank.SourcePreference!=0 || values[1].Rank.SourcePreference!=1 { t.Fatalf("world/history order=%#v",values) }
	single, singleReport := core.AttachAcceptedHistory(world,base,rows[:1],func(identity core.WorldRecallCandidateIdentity)(string,error){ return core.CanonicalDigest(identity) })
	singleRecent := single.Recall(core.WorldRecallQuery{Scope:core.WorldRecallObjectQuery,RecentOnly:true})
	if singleReport.Fatal || len(singleRecent)!=1 || singleRecent[0].CandidateID!=recent[0].CandidateID { t.Fatalf("frequency changed candidate identity: single=%#v grouped=%#v",singleRecent,recent) }
	second, secondReport := core.AttachAcceptedHistory(world,base,[]core.AcceptedHistoryRecord{rows[1],rows[0]},func(identity core.WorldRecallCandidateIdentity)(string,error){ return core.CanonicalDigest(identity) })
	if secondReport.Fatal || first.ID()!=second.ID() || !reflect.DeepEqual(recent,second.Recall(core.WorldRecallQuery{Scope:core.WorldRecallObjectQuery,RecentOnly:true})) { t.Fatalf("reorder drift: %#v %#v",report,secondReport) }
}

func TestAcceptedHistoryConflictingDuplicateDisablesOnlyHistory(t *testing.T) {
	command := core.CommandDefinition{Kind:core.CommandInputKind,CommandID:"demo",CommandVersion:"1",Name:"demo",Instruction:map[string]any{"version":"instruction.v1","op":"run","target":"demo","payload":map[string]any{}},Fields:[]core.CommandField{{Name:"value",Type:"string",Required:true,HistoryPolicy:core.HistoryPolicyRecall,Bind:"payload.value"}}}
	world := &core.JsonlWorld{Identity:&core.WorldDefinition{Kind:core.WorldDefinitionKind,WorldID:"history.duplicate"},Commands:[]core.CommandDefinition{command}}; _=world.RecomputeDigest()
	identify := func(identity core.WorldRecallCandidateIdentity)(string,error){ return core.CanonicalDigest(identity) }; base,_:=core.PrepareWorldRecall(world,identify); worldRef,_:=world.SelectedRef(); commandRef,_:=world.CommandRef("demo"); at,_:=time.Parse(time.RFC3339,"2026-07-16T00:00:00Z")
	makeRecord:=func(value,digest string)core.AcceptedHistoryRecord{input:=core.AcceptedInput{Kind:core.AcceptedInputKind,World:worldRef,Command:commandRef,Fields:[]core.AcceptedInputField{{Name:"value",Type:"string",Value:value}},SuppliedFieldCount:1,RecallComplete:true,RenderContract:core.CommandObjectMaterializerVersion};_ = input.BindInstructionDigest(digest);return core.AcceptedHistoryRecord{AcceptedID:"same",AcceptedAt:at,Provenance:core.CompileProvenance{Kind:core.CompileProvenanceKind,InputKind:core.CommandInputKind,World:worldRef,Command:&commandRef,InstructionDigest:digest},Input:input}}
	d1,_:=core.CanonicalDigest(map[string]any{"x":1});d2,_:=core.CanonicalDigest(map[string]any{"x":2});combined,report:=core.AttachAcceptedHistory(world,base,[]core.AcceptedHistoryRecord{makeRecord("a",d1),makeRecord("b",d2)},identify)
	if !report.Fatal || combined.ID()!=base.ID() { t.Fatalf("history did not degrade exactly: %#v",report) }
}

func TestAcceptedHistoryRejectsOversizedIncompleteFieldValues(t *testing.T) {
	command := core.CommandDefinition{Kind:core.CommandInputKind,CommandID:"demo",CommandVersion:"1",Name:"demo",Instruction:map[string]any{"version":"instruction.v1","op":"run","target":"demo","payload":map[string]any{}},Fields:[]core.CommandField{{Name:"value",Type:"string",HistoryPolicy:core.HistoryPolicyRecall,Bind:"payload.value"}}}
	world := &core.JsonlWorld{Identity:&core.WorldDefinition{Kind:core.WorldDefinitionKind,WorldID:"history.size"},Commands:[]core.CommandDefinition{command}}
	if err:=world.RecomputeDigest();err!=nil{t.Fatal(err)}
	identify:=func(identity core.WorldRecallCandidateIdentity)(string,error){return core.CanonicalDigest(identity)}
	base,err:=core.PrepareWorldRecall(world,identify);if err!=nil{t.Fatal(err)}
	worldRef,_:=world.SelectedRef();commandRef,_:=world.CommandRef("demo")
	instructionDigest,_:=core.CanonicalDigest(map[string]any{"id":"large","created_at":"2026-07-16T00:00:00Z"})
	input:=core.AcceptedInput{Kind:core.AcceptedInputKind,World:worldRef,Command:commandRef,Fields:[]core.AcceptedInputField{{Name:"value",Type:"string",Value:strings.Repeat("x",core.MaxAcceptedInputMaterialization+1)}},SuppliedFieldCount:2,RecallComplete:false,RenderContract:core.CommandObjectMaterializerVersion}
	if err:=input.BindInstructionDigest(instructionDigest);err!=nil{t.Fatal(err)}
	at,_:=time.Parse(time.RFC3339,"2026-07-16T00:00:00Z")
	record:=core.AcceptedHistoryRecord{AcceptedID:"large",AcceptedAt:at,Provenance:core.CompileProvenance{Kind:core.CompileProvenanceKind,InputKind:core.CommandInputKind,World:worldRef,Command:&commandRef,InstructionDigest:instructionDigest},Input:input}
	combined,report:=core.AttachAcceptedHistory(world,base,[]core.AcceptedHistoryRecord{record},identify)
	if combined.ID()!=base.ID() || report.Fatal || !hasHistoryFinding(report,"accepted-input-too-large") { t.Fatalf("oversized field survived: index=%q report=%#v",combined.ID(),report) }
}

func hasHistoryFinding(report core.AcceptedHistoryReport, code string) bool {
	for _,finding:=range report.Findings{if finding.Code==code{return true}}
	return false
}
