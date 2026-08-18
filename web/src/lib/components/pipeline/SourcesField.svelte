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

      // A Fibonacci sphere: even coverage with no clumping, and deterministic — a
      // reader who reloads the page sees the same shape, not a different random field.
      const group = new THREE.Group();
      const positions = new Float32Array(pointCount * 3);
      const colors = new Float32Array(pointCount * 3);
      const brand = readCssColor('--brand');
      const golden = Math.PI * (3 - Math.sqrt(5));
      for (let i = 0; i < pointCount; i++) {
        const y = 1 - (i / Math.max(1, pointCount - 1)) * 2;
        const radiusAtY = Math.sqrt(1 - y * y);
        const theta = golden * i;
        const r = 3.3;
        positions[i * 3] = Math.cos(theta) * radiusAtY * r;
        positions[i * 3 + 1] = y * r;
        positions[i * 3 + 2] = Math.sin(theta) * radiusAtY * r;
        // Per-point brightness jitter so the field has texture instead of reading as
        // one flat color — an even field of identical dots is what makes a point
        // cloud look like a placeholder rather than real, varied sources.
        const jitter = 0.6 + Math.random() * 0.5;
        colors[i * 3] = brand.r * jitter;
        colors[i * 3 + 1] = brand.g * jitter;
        colors[i * 3 + 2] = brand.b * jitter;
      }
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

      // The one node that never orbits: what all of it feeds into. A dim, larger halo
      // behind the solid core reads as a glow without a postprocessing bloom pass.
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
        const animate = () => {
          group.rotation.y += 0.0015;
          group.rotation.x = Math.sin(Date.now() / 8000) * 0.08;
          renderer.render(scene, camera);
          raf = requestAnimationFrame(animate);
        };
        animate();
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
  class="relative h-80 overflow-hidden rounded-xl border border-border bg-secondary/20 sm:h-[26rem]"
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
