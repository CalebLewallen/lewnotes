<script lang="ts">
	import type { Note } from '#lib/api';
	import { lineDiff } from '#lib/diff';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import * as CodeBlock from '$lib/components/ui/code-block/index.js';

	interface Props {
		server: Note;
		mine: string;
		busy?: boolean;
		onreload: () => void;
		onoverwrite: () => void;
	}

	let { server, mine, busy = false, onreload, onoverwrite }: Props = $props();

	const lines = $derived(lineDiff(server.content ?? server.body, mine));
	const added = $derived(lines.filter((line) => line.kind === 'add').length);
	const removed = $derived(lines.filter((line) => line.kind === 'del').length);
	let expanded = $state(false);
	const visible = $derived(expanded ? lines : lines.slice(0, 400));

	function variant(kind: 'same' | 'add' | 'del') {
		return kind === 'add' ? 'added' : kind === 'del' ? 'removed' : 'default';
	}
</script>

<Card.Root class="border-destructive/40 bg-destructive/5 mb-6">
	<Card.Header>
		<div class="flex flex-wrap items-start justify-between gap-3">
			<div>
				<Card.Title class="text-destructive">This note changed on the server</Card.Title>
				<Card.Description>
					Nothing was written. Review the difference, then reload or deliberately overwrite.
				</Card.Description>
			</div>
			<p class="text-destructive/80 font-mono text-[11px]">+{added} / −{removed} lines</p>
		</div>
	</Card.Header>

	<Card.Content>
		<CodeBlock.Root variant="destructive">
			<CodeBlock.Content class="max-h-80" wrap>
				{#each visible as line, index (index)}
					<CodeBlock.Line variant={variant(line.kind)}>{line.text}</CodeBlock.Line>
				{/each}
			</CodeBlock.Content>
			{#if lines.length > 400}
				<CodeBlock.Footer>
					<Button variant="ghost" size="xs" onclick={() => (expanded = !expanded)}>
						{expanded ? 'Show less' : 'Show full diff'}
					</Button>
				</CodeBlock.Footer>
			{/if}
		</CodeBlock.Root>
	</Card.Content>

	<Card.Footer class="flex flex-wrap gap-2">
		<Button variant="outline" onclick={onreload} disabled={busy}>Reload server version</Button>
		<Button variant="destructive" onclick={onoverwrite} disabled={busy}>
			{busy ? 'Writing…' : 'Overwrite with mine'}
		</Button>
	</Card.Footer>
</Card.Root>
