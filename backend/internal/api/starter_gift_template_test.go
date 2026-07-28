package api

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func starterGiftTestTemplateDir(t *testing.T) (string, string) {
	t.Helper()
	serverDir := t.TempDir()
	templateDir := filepath.Join(serverDir, "Pal", "Binaries", "Win64", "PalDefender", "Pals", "Templates")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return serverDir, templateDir
}

func TestReadStarterGiftTemplateInfoUsesJSONPalID(t *testing.T) {
	serverDir, templateDir := starterGiftTestTemplateDir(t)
	path := filepath.Join(templateDir, "BATTLE_MisleadingFilename.json")
	if err := os.WriteFile(path, []byte(`{"PalID":"WeaselDragon","Nickname":"Starter Chillet","Level":12}`), 0o644); err != nil {
		t.Fatal(err)
	}

	info := readStarterGiftTemplateInfo(serverDir, "BATTLE_MisleadingFilename.json", readStarterGiftTemplateIndexes(serverDir))
	if info.PalID != "WeaselDragon" {
		t.Fatalf("PalID = %q, want WeaselDragon", info.PalID)
	}
	if info.PalName != "" {
		t.Fatalf("PalName = %q without an index, want empty so the UI falls back to the filename", info.PalName)
	}
	if info.Nickname != "Starter Chillet" || info.Level != 12 {
		t.Fatalf("template metadata = %#v", info)
	}
	if info.ParseError != "" {
		t.Fatalf("unexpected parse error: %s", info.ParseError)
	}
}

func TestStarterGiftTemplateCatalogUsesChineseIndexAndMemberships(t *testing.T) {
	serverDir, templateDir := starterGiftTestTemplateDir(t)
	if err := os.WriteFile(filepath.Join(templateDir, "BATTLE__Anubis.json"), []byte(`{"PalID":"Anubis","Level":60}`), 0o644); err != nil {
		t.Fatal(err)
	}
	translation := `{
		"名称":"模板中英文对照索引",
		"模板":[{"分类":"战斗","模板名":"BATTLE__Anubis","文件名":"BATTLE__Anubis.json","中文名":"阿努比斯","PalID":"Anubis","英文名":"Anubis"}]
	}`
	if err := os.WriteFile(filepath.Join(templateDir, "模板中英文对照索引.json"), []byte(translation), 0o644); err != nil {
		t.Fatal(err)
	}
	graduate := `{"清单":[{"文件名":"BATTLE__Anubis.json","中文名":"阿努比斯","分类":"战斗"}]}`
	if err := os.WriteFile(filepath.Join(templateDir, "常用毕业帕鲁索引.json"), []byte(graduate), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, indexes := starterGiftTemplateCatalog(serverDir, []string{
		"BATTLE__Anubis.json",
		"模板中英文对照索引.json",
		"常用毕业帕鲁索引.json",
	})
	if len(catalog) != 1 {
		t.Fatalf("catalog = %#v, want one template and no index files", catalog)
	}
	info := catalog[0]
	if info.PalID != "Anubis" || info.PalName != "阿努比斯" || info.EnglishName != "Anubis" || info.Category != "战斗" {
		t.Fatalf("indexed template metadata = %#v", info)
	}
	wantMemberships := []string{"常用毕业帕鲁索引.json", "模板中英文对照索引.json"}
	if !reflect.DeepEqual(info.IndexNames, wantMemberships) {
		t.Fatalf("index memberships = %#v, want %#v", info.IndexNames, wantMemberships)
	}
	if len(indexes) != 2 || indexes[0].Name != "常用毕业帕鲁索引.json" || indexes[1].Name != "模板中英文对照索引.json" {
		t.Fatalf("index catalog = %#v", indexes)
	}
}

func TestReadStarterGiftTemplateInfoReportsMissingPalID(t *testing.T) {
	serverDir, templateDir := starterGiftTestTemplateDir(t)
	if err := os.WriteFile(filepath.Join(templateDir, "missing.json"), []byte(`{"Level":5}`), 0o644); err != nil {
		t.Fatal(err)
	}

	info := readStarterGiftTemplateInfo(serverDir, "missing.json", readStarterGiftTemplateIndexes(serverDir))
	if !strings.Contains(info.ParseError, "PalID") {
		t.Fatalf("parse error = %q", info.ParseError)
	}
	if info.PalID != "" || info.PalName != "" {
		t.Fatalf("unexpected Pal metadata: %#v", info)
	}
}

func TestStarterGiftTemplateFileNameRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../Anubis.json", `folder\\Anubis.json`, "Anubis.txt"} {
		if _, err := starterGiftTemplateFileName(name); err == nil {
			t.Fatalf("starterGiftTemplateFileName(%q) unexpectedly succeeded", name)
		}
	}
	if got, err := starterGiftTemplateFileName("Anubis"); err != nil || got != "Anubis.json" {
		t.Fatalf("starterGiftTemplateFileName(Anubis) = %q, %v", got, err)
	}
}

func TestStarterGiftTemplateCatalogReadsRichKeywordNamedIndex(t *testing.T) {
	serverDir, templateDir := starterGiftTestTemplateDir(t)
	if err := os.WriteFile(filepath.Join(templateDir, "WORK__Anubis.json"), []byte(`{"PalID":"Anubis","Level":60}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rich := `{
		"说明":"索引文件名必须包含 index 或 索引关键字",
		"版本":"Palworld 1.0",
		"模板":[{
			"分类":"工作","模板名":"WORK__Anubis","文件名":"WORK__Anubis.json","中文名":"阿努比斯","英文名":"Anubis","PalID":"Anubis",
			"用途分类":"手工作业＋采矿＋搬运","综合分级":"常用毕业","分类标签":["工作:手工作业","毕业:工作"],
			"毕业帕鲁":true,"毕业用途":["战斗","工作"],"当前模板是否毕业用途":true,"低阶与过渡":false,
			"夜间工作方式":"吸血鬼","词条中文":["恶魔之手","卓绝技艺","工匠精神","吸血鬼"]
		}]
	}`
	if err := os.WriteFile(filepath.Join(templateDir, "palworld-template-index.json"), []byte(rich), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, indexes := starterGiftTemplateCatalog(serverDir, []string{"WORK__Anubis.json", "palworld-template-index.json"})
	if len(catalog) != 1 || len(indexes) != 1 {
		t.Fatalf("catalog=%#v indexes=%#v", catalog, indexes)
	}
	info := catalog[0]
	if info.PalName != "阿努比斯" || info.UsageCategory != "手工作业＋采矿＋搬运" || info.OverallGrade != "常用毕业" {
		t.Fatalf("rich metadata=%#v", info)
	}
	if !info.GraduationPal || !info.CurrentGraduationUse || info.Transitional || info.NightWorkMode != "吸血鬼" {
		t.Fatalf("rich flags=%#v", info)
	}
	if !reflect.DeepEqual(info.GraduationUses, []string{"战斗", "工作"}) || !reflect.DeepEqual(info.PassiveNames, []string{"恶魔之手", "卓绝技艺", "工匠精神", "吸血鬼"}) {
		t.Fatalf("rich arrays=%#v", info)
	}
	if indexes[0].Name != "palworld-template-index.json" || indexes[0].Label != "Palworld 1.0" {
		t.Fatalf("keyword index=%#v", indexes[0])
	}
}

func TestStarterGiftTemplateCatalogIgnoresJSONWithoutIndexKeyword(t *testing.T) {
	serverDir, templateDir := starterGiftTestTemplateDir(t)
	if err := os.WriteFile(filepath.Join(templateDir, "WORK__Anubis.json"), []byte(`{"PalID":"Anubis","Level":60}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rich := `{"版本":"Palworld 1.0","模板":[{"分类":"工作","模板名":"WORK__Anubis","文件名":"WORK__Anubis.json","中文名":"阿努比斯","PalID":"Anubis"}]}`
	if err := os.WriteFile(filepath.Join(templateDir, "palworld-1.0.json"), []byte(rich), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, indexes := starterGiftTemplateCatalog(serverDir, []string{"WORK__Anubis.json", "palworld-1.0.json"})
	if len(indexes) != 0 {
		t.Fatalf("indexes=%#v", indexes)
	}
	if len(catalog) != 2 {
		t.Fatalf("catalog=%#v", catalog)
	}
}
