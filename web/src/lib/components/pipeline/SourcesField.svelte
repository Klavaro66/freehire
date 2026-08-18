<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { SectionLabel } from '$lib/ui';

  // The opening thesis of the page: before any of the five stages run, this is what
  // stage one actually faces — not one board, a field of them. `sources` and
  // `ats_platforms` come from the same live catalogue-scale snapshot /open reads (see
  // +page.server.ts), so the number here is never invented and never drifts from what
  // the rest of the site claims.
  //
  // Three.js is dynamically imported inside onMount — never in the initial bundle —
  // and the real numbers render as plain HTML underneath the canvas regardless of
  // whether WebGL, motion, or JavaScript itself is available. The canvas is a pure
  // enhancement layered on top; losing it loses nothing true about the page.
  let {
    sources,
    atsPlatforms,
  }: { sources: number | null; atsPlatforms: number | null } = $props();

  const nf = new Intl.NumberFormat('en');
  const sourcesLabel = $derived(sources != null ? nf.format(sources) : '200+');

  let container: HTMLDivElement;
  let canvas: HTMLCanvasElement;
  let cleanup: (() => void) | undefined;

  onMount(() => {
    let cancelled = false;

    (async () => {
      const pointCount = sources ?? 200;
      const THREE = await import('three');
      if (cancelled) return;

      // The token's raw value can be any CSS Color 4 syntax (this codebase's dark
      // tokens are oklch()), and a modern canvas fillStyle GETTER can hand the same
      // syntax straight back rather than normalizing it — which THREE.Color cannot
      // parse and silently leaves at its white default. Rasterizing one pixel and
      // reading it back is what actually forces concrete 8-bit sRGB, regardless of
      // the input color space or the browser's serialization quirks.
      const ctx = document.createElement('canvas').getContext('2d');
      const readCssColor = (name: string) => {
        const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
        if (!ctx || !raw) return new THREE.Color(0x5b6f00);
        ctx.fillStyle = raw;
        ctx.fillRect(0, 0, 1, 1);
        const pixel = ctx.getImageData(0, 0, 1, 1).data;
        return new THREE.Color((pixel[0] ?? 91) / 255, (pixel[1] ?? 111) / 255, (pixel[2] ?? 0) / 255);
      };

      const scene = new THREE.Scene();
      const camera = new THREE.PerspectiveCamera(50, 1, 0.1, 100);
      camera.position.set(0, 0.3, 5.2);
      camera.lookAt(0, 0, 0);

      let renderer: InstanceType<typeof THREE.WebGLRenderer>;
      try {
        renderer = new THREE.WebGLRenderer({ canvas, alpha: true, antialias: true });
      } catch {
        return; // No WebGL — the HTML figures underneath already say everything true.
      }
      renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));

      // A real gravity well, not a funnel: each point launches on a straight line
      // aimed near — not necessarily through — the core, with a random miss distance.
      // A wide miss just arcs past and exits the field to respawn on the far side; a
      // close miss gets bent in by the pull and is caught. Most points fly by; only
      // the ones that come close enough are the ones the core keeps.
      const group = new THREE.Group();
      const FIELD_R = 3.3;
      const CAPTURE_R = 0.3;
      const GRAVITY = 0.55;
      const MAX_ACCEL = 0.02;
      const positions = new Float32Array(pointCount * 3);
      const velocities = new Float32Array(pointCount * 3);
      const colors = new Float32Array(pointCount * 3);
      const brand = readCssColor('--brand');

      // A plain numeric-index read on a typed array is `number | undefined` under
      // this project's tsconfig; every read here is bounds-safe by construction
      // (i*3(+0..2) never exceeds pointCount*3), so this is purely a type escape
      // hatch, not a real fallback.
      const at = (arr: Float32Array, i: number) => arr[i] ?? 0;

      // Uniform-random point on a unit sphere (rejection-free: this parametrization
      // does not clump at the poles the way naive lat/long sampling would).
      const randomOnSphere = (): [number, number, number] => {
        const cosPhi = Math.random() * 2 - 1;
        const sinPhi = Math.sqrt(1 - cosPhi * cosPhi);
        const theta = Math.random() * Math.PI * 2;
        return [Math.cos(theta) * sinPhi, cosPhi, Math.sin(theta) * sinPhi];
      };

      // (Re)launches point i from the field's outer edge, aimed at a point offset
      // from the core by a random "miss distance" — the thing that makes most
      // trajectories a near-pass rather than a bullseye.
      const spawn = (i: number) => {
        const [sx, sy, sz] = randomOnSphere();
        const px = sx * FIELD_R;
        const py = sy * FIELD_R;
        const pz = sz * FIELD_R;
        positions[i * 3] = px;
        positions[i * 3 + 1] = py;
        positions[i * 3 + 2] = pz;

        const missDistance = Math.random() * Math.random() * 1.8; // biased toward small misses
        const [mx, my, mz] = randomOnSphere();
        const dx = mx * missDistance - px;
        const dy = my * missDistance - py;
        const dz = mz * missDistance - pz;
        const len = Math.hypot(dx, dy, dz) || 1;
        const speed = 0.018 + Math.random() * 0.02;
        velocities[i * 3] = (dx / len) * speed;
        velocities[i * 3 + 1] = (dy / len) * speed;
        velocities[i * 3 + 2] = (dz / len) * speed;
      };
      for (let i = 0; i < pointCount; i++) spawn(i);

      const geometry = new THREE.BufferGeometry();
      geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));
      geometry.setAttribute('color', new THREE.BufferAttribute(colors, 3));
      const material = new THREE.PointsMaterial({
        size: 0.075,
        vertexColors: true,
        transparent: true,
        opacity: 0.95,
        sizeAttenuation: true,
      });
      const points = new THREE.Points(geometry, material);
      group.add(points);
      scene.add(group);

      // The one node that never falls: what all of it feeds into. A dim, larger halo
      // behind the solid core reads as a glow without a postprocessing bloom pass, and
      // both briefly flare on every catch (see `pulse` in the animation loop below).
      const coreColor = readCssColor('--foreground');
      const halo = new THREE.Mesh(
        new THREE.SphereGeometry(0.34, 24, 24),
        new THREE.MeshBasicMaterial({ color: coreColor, transparent: true, opacity: 0.12 })
      );
      const core = new THREE.Mesh(
        new THREE.SphereGeometry(0.2, 24, 24),
        new THREE.MeshBasicMaterial({ color: coreColor })
      );
      scene.add(halo);
      scene.add(core);

      // Advances one point by one tick: gravity bends its velocity toward the core,
      // then it moves. Returns whether this tick caught it (came within CAPTURE_R,
      // in which case it's relaunched from the edge) or lost it (drifted past
      // FIELD_R outbound with too wide a miss to ever come back — also relaunched,
      // but that is NOT a catch, so the core's flare only fires on a real capture).
      const advance = (i: number, dt: number): 'caught' | 'lost' | null => {
        const px = at(positions, i * 3);
        const py = at(positions, i * 3 + 1);
        const pz = at(positions, i * 3 + 2);
        const distSq = px * px + py * py + pz * pz;
        const dist = Math.sqrt(distSq);

        if (dist < CAPTURE_R) {
          spawn(i);
          return 'caught';
        }

        const accel = Math.min(MAX_ACCEL, GRAVITY / Math.max(distSq, 0.05)) * dt;
        const vx = at(velocities, i * 3) - (px / dist) * accel;
        const vy = at(velocities, i * 3 + 1) - (py / dist) * accel;
        const vz = at(velocities, i * 3 + 2) - (pz / dist) * accel;
        velocities[i * 3] = vx;
        velocities[i * 3 + 1] = vy;
        velocities[i * 3 + 2] = vz;
        positions[i * 3] = px + vx * dt;
        positions[i * 3 + 1] = py + vy * dt;
        positions[i * 3 + 2] = pz + vz * dt;

        if (dist > FIELD_R * 1.15) {
          spawn(i);
          return 'lost';
        }
        return null;
      };

      // A fresh spawn() puts every point on the outer shell, which would render as a
      // hollow ring on the very first frame — so each point gets independently
      // fast-forwarded a random distance into its own flight before the first paint.
      for (let i = 0; i < pointCount; i++) {
        const warmup = Math.floor(Math.random() * 220);
        for (let k = 0; k < warmup; k++) advance(i, 1);
      }

      const positionAttr = geometry.getAttribute('position') as InstanceType<
        typeof THREE.BufferAttribute
      >;
      const colorAttr = geometry.getAttribute('color') as InstanceType<typeof THREE.BufferAttribute>;
      const step = (dt: number) => {
        let caught = 0;
        for (let i = 0; i < pointCount; i++) {
          if (advance(i, dt) === 'caught') caught++;
          const dist = Math.hypot(at(positions, i * 3), at(positions, i * 3 + 1), at(positions, i * 3 + 2));
          // Brighter as it nears the core — a point reads as flying toward the light,
          // not just sliding along a static gradient.
          const proximity = 1 - Math.min(1, dist / FIELD_R);
          const bright = 0.55 + proximity * 0.85;
          colors[i * 3] = brand.r * bright;
          colors[i * 3 + 1] = brand.g * bright;
          colors[i * 3 + 2] = brand.b * bright;
        }
        positionAttr.needsUpdate = true;
        colorAttr.needsUpdate = true;
        return caught;
      };
      step(0);

      // The text sits top-left (the canvas mask fades that corner out); push the whole
      // field toward the bottom-right so the core node — the brightest, largest thing
      // here — never sits behind a word regardless of how the box's aspect ratio
      // reflows the text block across breakpoints.
      const fieldOffset = new THREE.Vector3(1.2, -0.75, 0);
      group.position.copy(fieldOffset);
      core.position.copy(fieldOffset);
      halo.position.copy(fieldOffset);

      const resize = () => {
        const w = container.clientWidth;
        const h = container.clientHeight;
        renderer.setSize(w, h, false);
        camera.aspect = w / Math.max(1, h);
        camera.updateProjectionMatrix();
      };
      resize();
      const ro = new ResizeObserver(resize);
      ro.observe(container);

      const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      let raf = 0;
      if (reduceMotion) {
        renderer.render(scene, camera);
      } else {
        let pulse = 0;
        let last = performance.now();
        const animate = (now: number) => {
          // Clamped so a backgrounded tab regaining focus doesn't fast-forward the
          // fall by several seconds in one jump.
          const dt = Math.min(3, (now - last) / (1000 / 60));
          last = now;

          const caught = step(dt);
          if (caught > 0) pulse = 1;
          pulse *= 0.9;
          const flare = 1 + pulse * 0.5;
          core.scale.setScalar(flare);
          halo.scale.setScalar(flare);
          (halo.material as InstanceType<typeof THREE.MeshBasicMaterial>).opacity = 0.12 + pulse * 0.35;

          group.rotation.y += 0.0006 * dt;
          renderer.render(scene, camera);
          raf = requestAnimationFrame(animate);
        };
        raf = requestAnimationFrame(animate);
      }

      cleanup = () => {
        cancelAnimationFrame(raf);
        ro.disconnect();
        geometry.dispose();
        material.dispose();
        core.geometry.dispose();
        (core.material as InstanceType<typeof THREE.Material>).dispose();
        halo.geometry.dispose();
        (halo.material as InstanceType<typeof THREE.Material>).dispose();
        renderer.dispose();
      };
    })();

    return () => {
      cancelled = true;
      cleanup?.();
    };
  });

  onDestroy(() => cleanup?.());
</script>

<div
  bind:this={container}
  class="relative h-96 overflow-hidden sm:h-[32rem] lg:h-[38rem]"
>
  <canvas
    bind:this={canvas}
    class="absolute inset-0 h-full w-full"
    style="mask-image: radial-gradient(140% 140% at 0% 0%, transparent, transparent 25%, black 62%); -webkit-mask-image: radial-gradient(140% 140% at 0% 0%, transparent, transparent 25%, black 62%);"
    aria-hidden="true"
  ></canvas>
  <div class="relative z-10 flex h-full flex-col justify-start gap-2 p-6 sm:p-8">
    <SectionLabel text="where it starts" />
    <p class="font-mono text-4xl font-bold tracking-tight sm:text-5xl">
      {sourcesLabel}<span class="text-muted-foreground"> sources</span>
    </p>
    <p class="max-w-sm text-sm leading-relaxed text-muted-foreground">
      Career pages, ATS platforms and boards
      {atsPlatforms != null ? `— ${nf.format(atsPlatforms)} platforms among them` : ''} — each crawled
      on its own schedule. Every one is where stage one starts.
    </p>
  </div>
</div>
