package projectconfig

import (
 "fmt"
 "regexp"
 "strings"
)

var argumentPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// ValidateRule checks that a blacklist entry is a supported, exact-match rule.
// An unknown kind or opaque payload matcher must be rejected at save time:
// silently accepting an unenforceable block is worse than a clear error.
func ValidateRule(r Rule) error {
 if r.Tool=="" {return fmt.Errorf("ERR_RULE_UNSUPPORTED: tool must not be empty")}
 switch r.Kind {
 case "tool":
  if r.Path!="" || r.Equals!="" {return fmt.Errorf("ERR_RULE_UNSUPPORTED: tool rule cannot have arguments")}
 case "argument":
  if !argumentPath.MatchString(r.Path) {return fmt.Errorf("ERR_RULE_UNSUPPORTED: path must be a dot-separated object field")}
 default:
  return fmt.Errorf("ERR_RULE_UNSUPPORTED: unknown matcher kind %q",r.Kind)
 }
 return nil
}

// Denies returns whether an explicitly configured rule denies a concrete call.
// No automatic application-specific restrictions are added.
func Denies(blacklist MacBlacklist, tool string, args map[string]any) bool {
 for _,r:=range blacklist.Entries {
  if r.Tool!=tool {continue}
  if r.Kind=="tool" {return true}
  if r.Kind!="argument" {continue}
  var value any = args
  ok:=true
  for _,key:=range strings.Split(r.Path,".") {
   obj,isObj:=value.(map[string]any)
   if !isObj {ok=false;break}
   value,ok=obj[key]
   if !ok {break}
  }
  if !ok {continue}
  str,isString:=value.(string)
  if isString && str==r.Equals {return true}
 }
 return false
}
