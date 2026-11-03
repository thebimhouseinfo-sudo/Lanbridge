package projectconfig

import "testing"

func TestMatcherStartsEmptyAndDeniesOnlySelectedTools(t *testing.T) {
 b:=MacBlacklist{SchemaVersion:1,ProjectID:"generic-app",Entries:[]Rule{}}
 if Denies(b,"generic-app__create_document",nil){t.Fatal("default tool must not be denied")}
 b.Entries=[]Rule{{Kind:"tool",Tool:"generic-app__delete_document"}}
 if !Denies(b,"generic-app__delete_document",nil){t.Fatal("configured tool deny ineffective")}
 if Denies(b,"generic-app__create_document",nil){t.Fatal("unrelated tool denied")}
}

func TestArgumentRuleExactNestedPath(t *testing.T) {
 b:=MacBlacklist{SchemaVersion:1,ProjectID:"generic-app",Entries:[]Rule{{Kind:"argument",Tool:"generic-app__command",Path:"request.action",Equals:"open"}}}
 if !Denies(b,"generic-app__command",map[string]any{"request":map[string]any{"action":"open"}}){t.Fatal("match not denied")}
 if Denies(b,"generic-app__command",map[string]any{"request":map[string]any{"action":"close"}}){t.Fatal("incorrect match")}
 if Denies(b,"generic-app__command",map[string]any{"request":"open"}){t.Fatal("opaque nested field should not match")}
 if Denies(b,"unrelated__command",map[string]any{"request":map[string]any{"action":"open"}}){t.Fatal("wrong project tool denied")}
}

func TestUnsupportedMatcherRejected(t *testing.T) {
 for _,r:=range []Rule{{Kind:"regex",Tool:"a"},{Kind:"argument",Tool:"a",Path:"foo[0]",Equals:"x"},{Kind:"tool",Tool:"a",Path:"command"}} {
  if ValidateRule(r)==nil {t.Fatalf("unsupported rule accepted: %+v",r)}
 }
}
