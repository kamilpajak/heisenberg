<script lang="ts">
	import { auth } from '$stores/auth';
	import { goto } from '$app/navigation';
	import type { PageData } from './$types';

	export let data: PageData;

	$: if ($auth.isAuthenticated && !$auth.isLoading) {
		goto('/app');
	}
</script>

<svelte:head>
	<title>Heisenberg - AI-Powered Test Failure Analysis</title>
</svelte:head>

<div class="min-h-screen bg-gradient-to-b from-background to-muted">
	<header class="border-b border-border">
		<div class="container mx-auto px-4 py-4 flex justify-between items-center">
			<div class="flex items-center gap-2">
				<span class="text-2xl font-bold text-primary">Heisenberg</span>
			</div>
			<div class="flex gap-2">
				<a
					href="/api/auth/login"
					class="px-4 py-2 text-sm font-medium text-foreground hover:text-primary transition-colors"
				>
					Sign In
				</a>
				<a
					href="/api/auth/register"
					class="px-4 py-2 text-sm font-medium bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors"
				>
					Get Started
				</a>
			</div>
		</div>
	</header>

	<main class="container mx-auto px-4 py-16">
		<div class="max-w-3xl mx-auto text-center">
			<h1 class="text-5xl font-bold tracking-tight mb-6">
				AI-Powered <span class="text-primary">Test Failure</span> Analysis
			</h1>
			<p class="text-xl text-muted-foreground mb-8">
				Understand why your tests fail in seconds. Heisenberg analyzes Playwright reports
				and provides actionable root cause analysis with confidence scoring.
			</p>
			<div class="flex gap-4 justify-center">
				<a
					href="/api/auth/register"
					class="px-6 py-3 text-lg font-medium bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors"
				>
					Start Free Trial
				</a>
				<a
					href="https://github.com/kamilpajak/heisenberg"
					target="_blank"
					rel="noopener noreferrer"
					class="px-6 py-3 text-lg font-medium border border-border rounded-md hover:bg-accent transition-colors"
				>
					View on GitHub
				</a>
			</div>
		</div>

		<div class="grid md:grid-cols-3 gap-8 mt-16">
			<div class="p-6 rounded-lg border border-border bg-card">
				<h3 class="text-lg font-semibold mb-2">Instant Analysis</h3>
				<p class="text-muted-foreground">
					Upload your Playwright reports and get detailed root cause analysis in seconds.
				</p>
			</div>
			<div class="p-6 rounded-lg border border-border bg-card">
				<h3 class="text-lg font-semibold mb-2">Confidence Scoring</h3>
				<p class="text-muted-foreground">
					Know how confident the AI is in its diagnosis with transparent scoring.
				</p>
			</div>
			<div class="p-6 rounded-lg border border-border bg-card">
				<h3 class="text-lg font-semibold mb-2">Team History</h3>
				<p class="text-muted-foreground">
					Track analysis history, identify patterns, and share insights with your team.
				</p>
			</div>
		</div>
	</main>

	<footer class="border-t border-border mt-16">
		<div class="container mx-auto px-4 py-8 text-center text-muted-foreground">
			<p>&copy; 2026 Heisenberg. All rights reserved.</p>
		</div>
	</footer>
</div>
