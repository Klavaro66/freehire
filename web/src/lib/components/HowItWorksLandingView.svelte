<script lang="ts">
  import { resolve } from '$app/paths';
  import { Button, SectionLabel } from '$lib/ui';
  import Disclosure from '$lib/components/ghost/Disclosure.svelte';
  import SourcesField from '$lib/components/pipeline/SourcesField.svelte';
  import StageIllustration from '$lib/components/pipeline/StageIllustration.svelte';
  import { PIPELINE_STAGES } from '$lib/pipelineStages';
  import { PIPELINE_FAQ } from '$lib/pipelineFaq';

  // The page a reader reaches wanting to know why they can trust the catalogue: is it
  // current, is it the same ten jobs copied across ten boards, is a filter actually
  // narrowing anything. All three are answered by walking one posting through the same
  // five stages the backend runs it through — see docs/architecture.md and the root
  // AGENTS.md, which this page's copy is written from.
  let { scale }: { scale: { sources: number | null; companies: number | null } } = $props();
</script>

<article class="flex flex-col gap-14">
  <header class="flex flex-col items-start gap-5">
    <SectionLabel text="how it works" />
    <h1 class="max-w-2xl text-3xl font-semibold tracking-tight sm:text-4xl">
      Five steps before a posting reaches you.
    </h1>
    <p class="max-w-xl text-base leading-relaxed text-muted-foreground">
      Crawled, tagged, checked for duplicates, and rebuilt into the index you search —
      automatically, with no person touching an individual listing.
    </p>
    <div class="flex flex-wrap gap-3">
      <Button href={resolve('/jobs')} variant="primary" size="lg">Browse jobs</Button>
      <Button href={resolve('/open')} variant="ghost" size="lg">See the live catalogue</Button>
    </div>
  </header>

  <!-- The opening thesis, made visual: not one board, a field of them. -->
  <SourcesField sources={scale.sources} companies={scale.companies} />

  <!-- The signature: one example posting, followed through all five stages. Its title and
       facts stay the same specific example end to end (StageIllustration's five cards)
       so the sequence reads as one job's actual journey, not five unrelated icons. -->
  <section class="flex flex-col gap-6">
    <SectionLabel text="the pipeline" />
    <p class="max-w-2xl text-sm leading-relaxed text-muted-foreground">
      Follow one posting below — the picture at each step is that same job, at that stage.
    </p>

    <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-5">
      {#each PIPELINE_STAGES as stage (stage.key)}
        <div class="relative flex flex-col gap-3 overflow-hidden rounded-lg border border-border p-4">
          <div
            class="pointer-events-none absolute -right-2 -top-3 font-mono text-6xl font-bold text-border select-none"
            aria-hidden="true"
          >
            {stage.n}
          </div>
          <h3 class="relative text-sm font-semibold tracking-tight">{stage.title}</h3>
          <div class="relative rounded-md bg-secondary/40 p-3">
            <StageIllustration stage={stage.key} />
          </div>
          <p class="relative text-sm font-medium leading-snug">{stage.blurb}</p>
          <Disclosure summary="More detail" srSuffix={stage.title} class="relative">
            <p class="mt-2 text-sm leading-relaxed text-muted-foreground">{stage.detail}</p>
          </Disclosure>
        </div>
      {/each}
    </div>
  </section>

  <!-- FAQ; shares PIPELINE_FAQ with the page's JSON-LD. Collapsed with <details>, so every
       answer is still in the served HTML and the structured data cannot disagree with it. -->
  <section class="flex flex-col gap-4">
    <SectionLabel text="questions" />
    <div class="flex flex-col divide-y divide-border border-y border-border">
      {#each PIPELINE_FAQ as item (item.question)}
        <Disclosure summary={item.question} class="py-3">
          <p class="mt-2 max-w-3xl text-sm leading-relaxed text-muted-foreground">
            {item.answer}
          </p>
        </Disclosure>
      {/each}
    </div>
  </section>

  <section class="flex flex-col items-start gap-4 rounded-lg border border-border p-6">
    <h2 class="text-lg font-semibold tracking-tight">See it running on the real catalogue</h2>
    <p class="max-w-2xl text-sm leading-relaxed text-muted-foreground">
      This pipeline runs on every job you'll find here.
    </p>
    <div class="flex flex-wrap gap-3">
      <Button href={resolve('/jobs')} variant="primary">Browse jobs</Button>
      <Button href={resolve('/open')} variant="ghost">See the live catalogue</Button>
    </div>
  </section>
</article>
