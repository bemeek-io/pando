package docker

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
)

// volumeSizesFor is how long a reading of volume sizes is reused. Docker
// computes them by walking every volume on the host, which is the one part of
// a usage reading that grows with the host rather than with the app; once a
// minute is often enough for a number that changes slowly.
const volumeSizesFor = time.Minute

// Usage reads what an app's containers are using now (R-245).
//
// CPU is sampled by the daemon over about a second — the stats call without
// streaming waits for a second reading to diff against — so the containers are
// read in parallel, and the whole reading takes about a second however many
// parts the app has. A stopped container reports no CPU or memory, only the
// disk it wrote.
func (a *Adapter) Usage(ctx context.Context, ref api.BundleRef) (api.BundleUsage, error) {
	containers, err := a.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelBundle+"="+ref.BundleID)),
	})
	if err != nil {
		return api.BundleUsage{}, errs.Wrap(errs.AdapterUnavailable, "Could not read what is running.", err)
	}

	out := api.BundleUsage{Reported: time.Now().UTC(), Workloads: make([]api.WorkloadUsage, len(containers))}
	var wg sync.WaitGroup
	for i, c := range containers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out.Workloads[i] = a.workloadUsage(ctx, c.ID, c.Labels[labelWorkload])
		}()
	}
	wg.Wait()

	vols, err := a.cli.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", labelBundle+"="+ref.BundleID)),
	})
	if err == nil {
		sizes := a.volumeSizes(ctx)
		for _, v := range vols.Volumes {
			size, known := sizes[v.Name]
			if !known {
				size = -1
			}
			out.Volumes = append(out.Volumes, api.VolumeUsage{VolumeID: v.Labels["io.pando.volume"], Bytes: size})
		}
	}
	return out, nil
}

func (a *Adapter) workloadUsage(ctx context.Context, id, name string) api.WorkloadUsage {
	u := api.WorkloadUsage{Workload: name, DiskBytes: -1}

	// With sizes: SizeRw is what the container wrote to its own layer, which
	// is the disk it uses outside its volumes.
	inspect, _, err := a.cli.ContainerInspectWithRaw(ctx, id, true)
	if err != nil {
		return u
	}
	u.Running = inspect.State != nil && inspect.State.Running
	if inspect.SizeRw != nil {
		u.DiskBytes = *inspect.SizeRw
	}
	if inspect.HostConfig != nil {
		u.CPULimitMillis = int(inspect.HostConfig.NanoCPUs / 1_000_000)
		u.MemoryLimitBytes = inspect.HostConfig.Memory
	}
	if !u.Running {
		return u
	}

	stats, err := a.cli.ContainerStats(ctx, id, false)
	if err != nil {
		return u
	}
	defer func() { _ = stats.Body.Close() }()
	var s container.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&s); err != nil {
		return u
	}
	u.CPUMillis = cpuMillis(s)
	u.MemoryBytes = memoryInUse(s.MemoryStats)
	return u
}

// cpuMillis is the CPU a container used between the daemon's two readings, in
// thousandths of a core — the same arithmetic as `docker stats`, which reports
// it as a percentage of one core.
func cpuMillis(s container.StatsResponse) int {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if cpuDelta <= 0 || sysDelta <= 0 || cpus == 0 {
		return 0
	}
	return int(cpuDelta / sysDelta * cpus * 1000)
}

// memoryInUse is memory the container is using, less the page cache the
// kernel can take back — again as `docker stats` counts it, so the number
// matches what an operator sees there. cgroup v2 calls the reclaimable part
// inactive_file; v1, total_inactive_file.
func memoryInUse(m container.MemoryStats) int64 {
	used := m.Usage
	for _, key := range []string{"inactive_file", "total_inactive_file"} {
		if v, ok := m.Stats[key]; ok && v < used {
			used -= v
			break
		}
	}
	// Unsigned from the daemon, signed on the wire; no machine has 8 EiB.
	if used > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(used)
}

// volumeSizes is every volume's size on the host, by name, reused for
// volumeSizesFor (see there). An error leaves the sizes unknown rather than
// failing the reading: CPU and memory are still worth showing.
func (a *Adapter) volumeSizes(ctx context.Context) map[string]int64 {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	if a.volumeSizesAt.After(time.Now().Add(-volumeSizesFor)) {
		return a.volumeSizesCache
	}
	du, err := a.cli.DiskUsage(ctx, types.DiskUsageOptions{Types: []types.DiskUsageObject{types.VolumeObject}})
	if err != nil {
		return a.volumeSizesCache
	}
	sizes := map[string]int64{}
	for _, v := range du.Volumes {
		if v != nil && v.UsageData != nil && v.UsageData.Size >= 0 {
			sizes[v.Name] = v.UsageData.Size
		}
	}
	a.volumeSizesCache, a.volumeSizesAt = sizes, time.Now()
	return sizes
}
