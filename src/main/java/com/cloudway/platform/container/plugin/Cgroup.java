/**
 * Cloudway Platform
 * Copyright (c) 2012-2013 Cloudway Technology, Inc.
 * All rights reserved.
 */

package com.cloudway.platform.container.plugin;

import java.io.FileNotFoundException;
import java.io.IOException;
import java.io.RandomAccessFile;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import static java.nio.file.StandardCopyOption.*;
import java.util.Arrays;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.OptionalInt;
import java.util.stream.Stream;
import static java.util.Collections.*;
import static java.util.stream.Collectors.*;

import com.cloudway.platform.common.util.IO;
import jnr.constants.platform.Signal;
import jnr.posix.POSIX;
import jnr.posix.POSIXFactory;

import com.cloudway.platform.common.Config;
import com.cloudway.platform.common.util.FileUtils;
import com.cloudway.platform.common.util.RuntimeIOException;

public class Cgroup
{
    public static final String CG_ROOT;
    public static final String DEFAULT_CG_ROOT = "/cloudway";

    public static final List<String> CG_SUBSYSTEMS;
    public static final String DEFAULT_CG_SUBSYSTEMS = "cpu,cpuacct,memory,net_cls,freezer";

    private static final Path CGRULES = Paths.get("/etc/cgrules.conf");
    private static final Path CGCONFIG = Paths.get("/etc/cgconfig.conf");

    public static final Map<String, String> CG_MOUNTS;
    public static final Map<String, Path>   CG_PATHS;
    public static final Map<String, Object> CG_PARAMETERS;

    public static final boolean enabled;

    static {
        Config config = Config.getDefault();
        CG_ROOT = config.get("CGROUP_ROOT", DEFAULT_CG_ROOT);
        CG_SUBSYSTEMS = Arrays.asList(config.get("CGROUP_SUBSYSTEMS", DEFAULT_CG_SUBSYSTEMS).split(","));

        Map<String, String> mounts = new LinkedHashMap<>();
        Map<String, Path>   paths = new LinkedHashMap<>();
        Map<String, Object> params = new LinkedHashMap<>();
        try {
            init_cgmounts(mounts);
            init_cgpaths(mounts, paths);
            init_cgparameters(paths, params);
        } catch (Exception ex) {
            // TODO: log warning
            mounts.clear();
            paths.clear();
            params.clear();
        }

        CG_MOUNTS = unmodifiableMap(mounts);
        CG_PATHS = unmodifiableMap(paths);
        CG_PARAMETERS = unmodifiableMap(params);

        enabled = !CG_PATHS.isEmpty();
    }

    private static void init_cgmounts(Map<String, String> mounts)
        throws IOException
    {
        try (Stream<String> lines = Files.lines(Paths.get("/proc/mounts"))) {
            lines.forEach(line -> {
                // fs_spec fs_file fs_vtype fs_mntopts fs_freq fs_passno
                String[]     fs         = line.split("\\s+");
                String       fs_file    = fs[1];
                String       fs_vtype   = fs[2];
                List<String> fs_mntopts = Arrays.asList(fs[3].split(","));

                if (fs_vtype.equals("cgroup")) {
                    CG_SUBSYSTEMS.stream()
                        .filter(fs_mntopts::contains)
                        .forEach(subsys -> mounts.put(subsys, fs_file));
                }
            });
        }
    }

    private static void init_cgpaths(Map<String, String> mounts, Map<String, Path> paths)
        throws IOException
    {
        IO.caught(() -> mounts.forEach(IO.wrap((subsys, fs) -> paths.put(subsys, root_cgpath(fs)))));
    }

    private static Path root_cgpath(String fs)
        throws IOException
    {
        Path path = Paths.get(fs, CG_ROOT);
        if (!Files.exists(path))
            FileUtils.mkdir(path, 0755);
        return path;
    }

    private static void init_cgparameters(Map<String, Path> paths, Map<String, Object> params)
        throws IOException
    {
        IO.caught(() -> paths.forEach(IO.wrap((subsys, path) -> {
            try (Stream<Path> files = FileUtils.find(path, 1, subsys + ".*")) {
                files
                    .sorted(Comparator.comparingInt(Cgroup::count_dots))
                    .forEach(file -> {
                        try {
                            String pval = FileUtils.read(file).trim();
                            params.put(file.getFileName().toString(), parse_cgparam(pval));
                        } catch (IOException ex) {
                            // ignore unreadable file
                        }
                    });
            }
        })));
    }

    private static int count_dots(Path path) {
        String filename = path.getFileName().toString();
        int c = 0, len = filename.length();
        for (int i = 0; i < len; i++) {
            if (filename.charAt(i) == '.') {
                c++;
            }
        }
        return c;
    }

    private static Object parse_cgparam(String pval) {
        if (pval.indexOf('\n') == -1 && pval.indexOf(' ') == -1) {
            return pval;
        } else {
            Map<String,String> pmap = new LinkedHashMap<>();
            Arrays.stream(pval.split("\n")).forEach(line -> {
                int i = line.indexOf(' ');
                if (i > 0) {
                    pmap.put(line.substring(0, i), line.substring(i+1));
                }
            });
            return pmap;
        }
    }

    @SuppressWarnings("unchecked")
    private static String format_cgparam(Object val) {
        if (val instanceof Map) {
            return ((Map<String,Object>)val).entrySet().stream()
                .map(e -> e.getKey() + " " + e.getValue())
                .collect(joining("\n"));
        } else {
            return String.valueOf(val);
        }
    }

    private final String uuid;
    private final int uid;
    private final Map<String, Path> cgpaths;

    public Cgroup(String uuid, int uid) {
        this.uuid = uuid;
        this.uid = uid;

        cgpaths = CG_PATHS.entrySet().stream().collect(
            toMap(Map.Entry::getKey, e -> e.getValue().resolve(uuid)));
    }


    /**
     * Create a cgroup namespace for the application container.
     */
    public void create(Map<String,Object> defaults)
        throws IOException
    {
        Map<String, Map<String,Object>> newcfg = new LinkedHashMap<>();
        Map<String, Object> to_store = new LinkedHashMap<>();

        Map<String, Object> perm = new LinkedHashMap<>();
        perm.put("task", newmap("uid", uid, "gid", uid));
        perm.put("admin", newmap("uid", "root", "gid", "root"));
        newcfg.put("perm", perm);

        CG_SUBSYSTEMS.forEach(s -> newcfg.computeIfAbsent(s, x -> new LinkedHashMap<>()));

        CG_PARAMETERS.keySet().forEach(k -> {
            Object v = defaults.get(k);
            if (v != null) {
                String subsys = k.substring(0, k.indexOf('.'));
                if (newcfg.containsKey(subsys)) {
                    newcfg.get(subsys).put(k, v);
                    to_store.put(k, v);
                }
            }
        });

        cgcreate();
        update_cgconfig(newcfg);
        update_cgrules(true);
        store(to_store);
        cgclassify();
    }

    private static Map<String,Object> newmap(Object... args) {
        Map<String,Object> map = new LinkedHashMap<>();
        for (int i = 0; i < args.length; i += 2) {
            map.put((String)args[i], args[i+1]);
        }
        return map;
    }

    /**
     * Delete a cgroup namespace for the application container.
     */
    public void delete() throws IOException {
        update_cgconfig(null);
        update_cgrules(false);
        cgdelete();
    }

    /**
     * Fetch parameters for a specific uuid, or a map of key=>value
     * for all parameters for the application container.
     */
    public Map<String, Object> fetch() throws IOException {
        Map<String,Object> vals = new LinkedHashMap<>();

        IO.caught(() -> CG_PARAMETERS.forEach((param, defval) -> {
            String subsys = param.substring(0, param.indexOf('.'));
            Path path = cgpaths.get(subsys);

            if (path == null || !Files.exists(path)) {
                throw new RuntimeException("User does not exist in cgroups: " + uuid);
            }

            try {
                String val = FileUtils.read(path.resolve(param));
                vals.put(param, parse_cgparam(val));
            } catch (IOException ex) {
                throw new RuntimeIOException(ex);
            }
        }));

        return vals;
    }

    public void store(Map<String,Object> vals) throws IOException {
        try {
            CG_PARAMETERS.forEach((param, val) -> {
                val = vals.get(param);
                if (val != null) {
                    String subsys = param.substring(0, param.indexOf('.'));
                    Path path = cgpaths.get(subsys);
                    try {
                        if (path == null || !Files.exists(path)) {
                            throw new IOException("User does not exist in cgroups: " + uuid);
                        } else {
                            FileUtils.write(path.resolve(param), format_cgparam(val));
                        }
                    } catch (IOException ex) {
                        throw new RuntimeIOException(ex);
                    }
                }
            });
        } catch (RuntimeIOException ex) {
            throw ex.getCause();
        }
    }

    /**
     * Distribute this user's processes into their cgroup.
     */
    public void cgclassify() throws IOException {
        String tasks = threads()
            .filter(p -> p.uid == uid)
            .map(p -> String.valueOf(p.tid))
            .collect(joining("\n"));

        if (tasks.length() != 0) {
            IO.caught(() -> cgpaths.forEach(IO.wrap(
                (subsys, path) -> FileUtils.write(path.resolve("tasks"), tasks))));
        }
    }

    static class Task {
        int pid, tid;
        String name;
        int uid, gid;
    }

    private Stream<Task> processes() throws IOException {
        try (Stream<Path> ps = Files.list(Paths.get("/proc"))) {
            return ps.filter(Cgroup::is_pid_file)
                     .map(IO.wrap(Cgroup::read_process_info))
                     .collect(toList())
                     .stream();
        } catch (RuntimeIOException ex) {
            throw ex.getCause();
        }
    }

    private Stream<Task> threads() throws IOException {
        return IO.caught(() -> processes().flatMap(p -> {
            try (Stream<Path> ts = Files.list(Paths.get("/proc", String.valueOf(p.pid), "task"))) {
                return ts.filter(Cgroup::is_pid_file)
                         .map(file -> get_thread_info(p, file))
                         .collect(toList())
                         .stream();
            } catch (IOException ex) {
                throw new RuntimeIOException(ex);
            }
        }));
    }

    private static Task read_process_info(Path file)
        throws IOException
    {
        Task t = new Task();
        t.pid = Integer.parseInt(file.getFileName().toString());

        try (Stream<String> lines = Files.lines(file.resolve("status"))) {
            lines.forEach(line -> {
                int i = line.indexOf(':');
                if (i != -1) {
                    String token = line.substring(0, i).trim();
                    String values = line.substring(i + 1).trim();
                    switch (token) {
                    case "Name":
                        t.name = values;
                        break;
                    case "Uid":
                        parse_uid(values).ifPresent(uid -> t.uid = uid);
                        break;
                    case "Gid":
                        parse_uid(values).ifPresent(gid -> t.gid = gid);
                        break;
                    }
                }
            });
        }

        return t;
    }

    private static Task get_thread_info(Task proc, Path file) {
        Task t = new Task();
        t.pid  = proc.pid;
        t.tid  = Integer.parseInt(file.getFileName().toString());
        t.name = proc.name;
        t.uid  = proc.uid;
        t.gid  = proc.gid;
        return t;
    }

    private static boolean is_pid_file(Path file) {
        String s = file.getFileName().toString();
        int len = s.length();
        for (int i = 0; i < len; i++) {
            if (!Character.isDigit(s.charAt(i))) {
                return false;
            }
        }
        return true;
    }

    private static OptionalInt parse_uid(String s) {
        String[] a = s.split("\\s+");
        if (a.length > 0) {
            return OptionalInt.of(Integer.parseInt(a[0]));
        } else {
            return OptionalInt.empty();
        }
    }

    private void cgcreate() throws IOException {
        IO.caught(() ->
            cgpaths.forEach(IO.wrap((subsys, path) -> {
                if (!Files.exists(path))
                    FileUtils.mkdir(path, 0755);
                FileUtils.chown(path.resolve("tasks"), uuid, uuid);
            })));
    }

    private void cgdelete() throws IOException {
        IO.caught(() -> cgpaths.forEach(IO.wrap((subsys, path) -> Files.deleteIfExists(path))));
    }

    /**
     * Update the cgrules.conf file. This removes the requested uuid
     * and re-adds it at the end if a new path is provided.
     */
    private void update_cgrules(boolean recreate) throws IOException {
        overwrite_with_safe_swap(CGRULES, (in, out) -> {
            String line;
            while ((line = in.readLine()) != null) {
                if (!line.startsWith(uuid)) {
                    out.writeBytes(line);
                    out.write('\n');
                }
            }

            if (recreate) {
                out.writeBytes(uuid + '\t' +
                               String.join(",", CG_SUBSYSTEMS) + '\t' +
                               CG_ROOT + '/' + uuid + '\n');
            }
        });

        // reload cgrules.conf
        POSIX posix = POSIXFactory.getPOSIX();
        try (Stream<Task> ps = processes()) {
            ps.filter(p -> "cgrulesengd".equals(p.name))
              .forEach(p -> posix.kill(p.pid, Signal.SIGUSR2.intValue()));
        }
    }

    /**
     * Update the cgconfig.conf file. This removes the requested
     * path and re-adds it at the end if a new configuration
     * is provided.
     */
    private void update_cgconfig(Map<String,?> newcfg)
        throws IOException
    {
        String prefix = "group " + CG_ROOT + "/" + uuid + " ";

        overwrite_with_safe_swap(CGCONFIG, (in, out) -> {
            String line;
            while ((line = in.readLine()) != null) {
                if (!line.startsWith(prefix)) {
                    out.writeBytes(line);
                    out.write('\n');
                }
            }

            if (newcfg != null) {
                out.writeBytes(prefix);
                out.writeBytes(gen_cgconfig(newcfg));
                out.write('\n');
            }
        });
    }

    /**
     * Generate configuration stanzas for cgconfig.conf from a map.
     */
    @SuppressWarnings("unchecked")
    private static String gen_cgconfig(Object data) {
        StringBuilder rbuf = new StringBuilder();
        if (data instanceof Map) {
            rbuf.append('{');
            ((Map<String,?>)data).forEach((k, v) -> {
                rbuf.append(' ').append(k).append(' ');
                rbuf.append(gen_cgconfig(v));
            });
            rbuf.append('}');
        } else {
            rbuf.append("= ").append(data).append("; ");
        }
        return rbuf.toString();
    }

    private static void overwrite_with_safe_swap(Path file,
            IO.BiConsumer<RandomAccessFile, RandomAccessFile> action)
        throws IOException
    {
        Path tempfile = file.resolveSibling(file.getFileName() + "~");

        RandomAccessFile in;
        try {
            in = new RandomAccessFile(file.toFile(), "r");
        } catch (FileNotFoundException ex) {
            in = new RandomAccessFile("/dev/null", "r");
        }

        try (RandomAccessFile out = new RandomAccessFile(tempfile.toFile(), "rw")) {
            action.accept(in, out);
        } finally {
            in.close();
        }

        Files.move(tempfile, file, REPLACE_EXISTING, ATOMIC_MOVE);
    }
}
