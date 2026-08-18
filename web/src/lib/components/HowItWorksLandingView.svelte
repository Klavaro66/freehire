<script lang="ts">
  import { resolve } from '$app/paths';
  import { ArrowRight } from '@lucide/svelte';
  import { Button, SectionLabel } from '$lib/ui';
  import Disclosure from '$lib/components/ghost/Disclosure.svelte';
  import StageIllustration from '$lib/components/pipeline/StageIllustration.svelte';
  import { PIPELINE_STAGES } from '$lib/pipelineStages';
  import { PIPELINE_FAQ } from '$lib/pipelineFaq';

  // The page a reader reaches wanting to know why they can trust the catalogue: is it
  // current, is it the same ten jobs copied across ten boards, is a filter actually
  // narrowing anything. All three are answered by walking one posting through the same
  // five stages the backend runs it through — see docs/architecture.md and the root
  // AGENTS.md, which this page's copy is written from.
</script>

<article class="flex flex-col gap-16">
  <header class="flex flex-col items-start gap-6">
    <SectionLabel text="how it works" />
    <h1 class="max-w-2xl text-3xl font-semibold tracking-tight sm:text-4xl">
      One posting, five steps, before you ever see it.
    </h1>
    <p class="max-w-2xl text-base leading-relaxed text-muted-foreground">
      Every job in the catalogue is crawled straight from where a company actually posted
      it, tagged against a fixed dictionary, checked for duplicates against every other
      board, and rebuilt into the index you search — automatically, continuously, with no
      person touching an individual listing.
    </p>
    <div class="flex flex-wrap gap-3">
      <Button href={resolve('/jobs')} variant="primary" size="lg">Browse jobs</Button>
      <Button href={resolve('/open')} variant="ghost" size="lg">See the live catalogue</Button>
    </div>
  </header>

  <!-- The signature: one example posting, followed through all five stages. Its title and
       facts stay the same specific example end to end (StageIllustration's five cards)
       so the sequence reads as one job's actual journey, not five unrelated icons. -->
  <section class="flex flex-col gap-6">
    <SectionLabel text="the pipeline" />
    <p class="max-w-2xl text-sm leading-relaxed text-muted-foreground">
      Follow one posting below — the picture at each step is that same job, at that stage.
    </p>

    <div class="flex flex-col items-stretch gap-2 lg:flex-row lg:items-stretch lg:gap-0">
      {#each PIPELINE_STAGES as stage, i (stage.key)}
        <div class="flex flex-1 flex-col gap-3 rounded-lg border border-border p-4">
          <div class="flex items-baseline gap-2">
            <span class="font-mono text-xs text-muted-foreground">{stage.n}</span>
            <h3 class="text-sm font-semibold tracking-tight">{stage.title}</h3>
          </div>
          <StageIllustration stage={stage.key} />
          <p class="text-sm leading-relaxed text-muted-foreground">{stage.blurb}</p>
          <Disclosure summary="More detail" srSuffix={stage.title}>
            <p class="mt-2 text-sm leading-relaxed text-muted-foreground">{stage.detail}</p>
          </Disclosure>
        </div>
        {#if i < PIPELINE_STAGES.length - 1}
          <div class="flex items-center justify-center py-1 lg:px-1 lg:py-0" aria-hidden="true">
            <ArrowRight class="size-4 shrink-0 rotate-90 text-muted-foreground/40 lg:rotate-0" />
          </div>
        {/if}
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
      The pipeline runs on every job you'll find here. The open startup page shows the
      current figures — jobs, companies and sources — straight from the same API.
    </p>
    <div class="flex flex-wrap gap-3">
      <Button href={resolve('/jobs')} variant="primary">Browse jobs</Button>
      <Button href={resolve('/open')} variant="ghost">See the live catalogue</Button>
    </div>
  </section>
</article>
