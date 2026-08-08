package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSelectModelGroupPriorityAndManualTieOrder(t *testing.T) {
	groups := []ModelGroup{
		{ID: 1, Name: "late", Enabled: true, Priority: 80, Models: []string{"gpt-*"}, ProviderIDs: []int64{1}},
		{ID: 2, Name: "first tie", Enabled: true, Priority: 10, Models: []string{"gpt-*"}, ProviderIDs: []int64{2}},
		{ID: 3, Name: "second tie", Enabled: true, Priority: 10, Models: []string{"gpt-5*"}, ProviderIDs: []int64{3}},
	}
	selected := SelectModelGroup(groups, "gpt-5.6")
	if selected == nil || selected.ID != 2 {
		t.Fatalf("同优先级应按存储的手动顺序先到先得: %#v", selected)
	}

	groups[1].Enabled = false
	selected = SelectModelGroup(groups, "gpt-5.6")
	if selected == nil || selected.ID != 3 {
		t.Fatalf("禁用分组应被跳过: %#v", selected)
	}

	groups[2].ProviderIDs = nil
	selected = SelectModelGroup(groups, "gpt-5.6")
	if selected == nil || selected.ID != 1 {
		t.Fatalf("不完整分组应被跳过: %#v", selected)
	}
}

func TestModelGroupWildcardIsCaseSensitive(t *testing.T) {
	group := ModelGroup{Models: []string{"GPT-*"}}
	if !group.MatchesModel("GPT-5") {
		t.Fatal("单星号规则应匹配")
	}
	if group.MatchesModel("gpt-5") {
		t.Fatal("模型规则必须区分大小写")
	}
	if err := validateModelGroups([]ModelGroup{{
		ID: 1, Name: "bad", Enabled: true, Priority: 1,
		Models: []string{"a**b"}, ProviderIDs: []int64{1},
	}}); err == nil {
		t.Fatal("多星号规则必须拒绝保存")
	}
}

func TestModelGroupMigrationAndLifecycle(t *testing.T) {
	setupRenameTestEnv(t)
	path, err := providerFilePath(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"providers":[
		{"id":11,"name":"A","apiUrl":"https://a.example","apiKey":"a","enabled":true,"level":2,"maxConcurrency":1,"supportedModels":{"gpt-*":true}},
		{"id":12,"name":"B","apiUrl":"https://b.example","apiKey":"b","enabled":true}
	]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	service := NewProviderService()
	providers, groups, generation, err := service.LoadConfigurationWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Name != defaultModelGroupName || groups[0].Priority != 100 ||
		len(groups[0].Models) != 1 || groups[0].Models[0] != "*" ||
		len(groups[0].ProviderIDs) != 2 || groups[0].ProviderIDs[0] != 11 || groups[0].ProviderIDs[1] != 12 {
		t.Fatalf("旧配置应迁移为包含现有 Provider 的普通 fallback 分组: %#v", groups)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"supportedModels", "maxConcurrency", "\"level\""} {
		if strings.Contains(string(data), removed) {
			t.Fatalf("旧路由字段 %s 应在迁移保存时移除: %s", removed, data)
		}
	}

	// A provider added after group creation remains ungrouped.
	providers = append(providers, Provider{ID: 13, Name: "C", APIURL: "https://c.example", APIKey: "c", Enabled: true})
	if _, _, err := service.UpdateProviders(CodexPlatform, generation, func([]Provider) ([]Provider, error) {
		return providers, nil
	}); err != nil {
		t.Fatal(err)
	}
	_, groups, generation, err = service.LoadConfigurationWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups[0].ProviderIDs) != 2 {
		t.Fatalf("后续新增 Provider 不应自动加入已有分组: %#v", groups[0].ProviderIDs)
	}

	// Provider deletion prunes references without deleting the now-smaller group.
	if _, _, err := service.UpdateProviders(CodexPlatform, generation, func(current []Provider) ([]Provider, error) {
		return current[1:], nil
	}); err != nil {
		t.Fatal(err)
	}
	_, groups, generation, err = service.LoadConfigurationWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].ProviderIDs) != 1 || groups[0].ProviderIDs[0] != 12 {
		t.Fatalf("删除 Provider 应原子清理分组引用并保留分组: %#v", groups)
	}

	// An explicitly empty group array is user state, not a migration marker.
	if _, err := service.UpdateModelGroups(CodexPlatform, generation, []ModelGroup{}); err != nil {
		t.Fatal(err)
	}
	_, groups, _, err = service.LoadConfigurationWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if groups == nil || len(groups) != 0 {
		t.Fatalf("显式空分组不得重新创建 fallback: %#v", groups)
	}
}

func TestExistingModelGroupsDropLegacyProviderRoutingFields(t *testing.T) {
	setupRenameTestEnv(t)
	path, err := providerFilePath(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{
		"providers":[{"id":11,"name":"A","apiUrl":"https://a.example","apiKey":"a","enabled":true,"level":2,"maxConcurrency":1,"supportedModels":{"gpt-*":true}}],
		"modelGroups":[{"id":7,"name":"Existing","enabled":true,"priority":20,"models":["gpt-*"],"providerIds":[11]}]
	}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	service := NewProviderService()
	_, groups, _, err := service.LoadConfigurationWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != 7 {
		t.Fatalf("已有分组应原样保留: %#v", groups)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"supportedModels", "maxConcurrency", "\"level\""} {
		if strings.Contains(string(data), removed) {
			t.Fatalf("已有分组配置中的旧字段 %s 应在加载时移除: %s", removed, data)
		}
	}
}

func TestRelayDoesNotFallThroughMatchingGroups(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	var firstHits, laterHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstHits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer first.Close()
	later := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		laterHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"should-not-run"}`))
	}))
	defer later.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders(CodexPlatform, []Provider{
		{ID: 1, Name: "Bound failure", APIURL: first.URL, APIKey: "k1", Enabled: true},
		{ID: 2, Name: "Later success", APIURL: later.URL, APIKey: "k2", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, generation, err := providerService.LoadConfigurationWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	_, err = providerService.UpdateModelGroups(CodexPlatform, generation, []ModelGroup{
		{ID: 10, Name: "first", Enabled: true, Priority: 1, Models: []string{"m"}, ProviderIDs: []int64{1}},
		{ID: 20, Name: "later", Enabled: true, Priority: 2, Models: []string{"m"}, ProviderIDs: []int64{2}},
	})
	if err != nil {
		t.Fatal(err)
	}

	relay := newTestRelayService(providerService)
	router := gin.New()
	router.POST("/responses", relay.proxyHandler(CodexPlatform, "/responses"))

	request := httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"m"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("首个匹配分组内失败时请求必须失败: %s", response.Body.String())
	}
	if firstHits.Load() == 0 || laterHits.Load() != 0 {
		t.Fatalf("不得越过已绑定分组: first=%d later=%d", firstHits.Load(), laterHits.Load())
	}

	request = httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"other"}`))
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("无匹配分组应返回 404: %d %s", response.Code, response.Body.String())
	}
}

func TestModelsRouteUsesStarGroupAndProviderOrder(t *testing.T) {
	setupRenameTestEnv(t)
	gin.SetMode(gin.TestMode)
	var specificHits, fallbackHits atomic.Int32
	specific := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		specificHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"wrong"}]}`))
	}))
	defer specific.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		fallbackHits.Add(1)
		if request.URL.Path != "/v1/models" {
			t.Errorf("模型列表路径错误: %s", request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"selected"}]}`))
	}))
	defer fallback.Close()

	providerService := NewProviderService()
	if err := providerService.SaveProviders(CodexPlatform, []Provider{
		{ID: 1, Name: "Specific", APIURL: specific.URL, APIKey: "k1", Enabled: true},
		{ID: 2, Name: "Fallback", APIURL: fallback.URL, APIKey: "k2", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, generation, err := providerService.LoadConfigurationWithGen(CodexPlatform)
	if err != nil {
		t.Fatal(err)
	}
	_, err = providerService.UpdateModelGroups(CodexPlatform, generation, []ModelGroup{
		{ID: 10, Name: "specific", Enabled: true, Priority: 1, Models: []string{"gpt-*"}, ProviderIDs: []int64{1}},
		{ID: 20, Name: "fallback", Enabled: true, Priority: 2, Models: []string{"*"}, ProviderIDs: []int64{2, 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	relay := newTestRelayService(providerService)
	router := gin.New()
	router.GET("/v1/models", relay.modelsHandler(CodexPlatform))
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "selected") {
		t.Fatalf("模型列表应由 * 分组首个 Provider 返回: %d %s", response.Code, response.Body.String())
	}
	if fallbackHits.Load() != 1 || specificHits.Load() != 0 {
		t.Fatalf("应忽略不匹配字面量 * 的具体规则: specific=%d fallback=%d", specificHits.Load(), fallbackHits.Load())
	}
}
