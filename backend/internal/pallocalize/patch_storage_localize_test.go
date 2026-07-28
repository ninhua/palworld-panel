package pallocalize

import "testing"

func TestPatchStorageLocalization(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "item icon", got: ItemIcon("Stone"), want: "stone"},
		{name: "container technology", got: ContainerName("Infra_ItemChest_Grade_02"), want: "金属箱"},
		{name: "container map object", got: ContainerName("ItemChest_03"), want: "精炼金属箱"},
		{name: "unknown item icon", got: ItemIcon("FutureItem_1"), want: ""},
		{name: "unknown container", got: ContainerName("FutureStorage_1"), want: "FutureStorage_1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("got %q, want %q", test.got, test.want)
			}
		})
	}
}
