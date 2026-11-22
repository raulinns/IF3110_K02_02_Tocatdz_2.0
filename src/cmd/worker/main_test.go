package main

import "testing"

func TestWorkerOptions(t *testing.T) {
	env := map[string]string{"WORKER_ID": "worker-a", "WORKER_CONCURRENCY": "4"}
	options, err := workerOptions(func(key string) string { return env[key] })
	if err != nil || options.Owner != "worker-a" || options.Concurrency != 4 {
		t.Fatalf("options = %+v, %v", options, err)
	}
	options, err = workerOptions(func(string) string { return "" })
	if err != nil || options.Owner == "" || options.Concurrency != defaultConcurrency {
		t.Fatalf("default options = %+v, %v", options, err)
	}
	for _, value := range []string{"0", "65", "x"} {
		if _, err := workerOptions(func(key string) string {
			if key == "WORKER_CONCURRENCY" {
				return value
			}
			return ""
		}); err == nil {
			t.Fatalf("WORKER_CONCURRENCY=%s accepted", value)
		}
	}
}
