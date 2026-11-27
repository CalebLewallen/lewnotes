import { getContext, setContext } from 'svelte';

import type { TreeNode } from './api';

const VAULT_TREE = Symbol('lewnotes.vault-tree');

export function provideVaultTree(getTree: () => TreeNode[]): void {
  setContext(VAULT_TREE, getTree);
}

export function useVaultTree(): () => TreeNode[] {
  return getContext<() => TreeNode[]>(VAULT_TREE) ?? (() => []);
}

export function notePaths(nodes: TreeNode[], out: string[] = []): string[] {
  for (const node of nodes) {
    if (node.type === 'note') out.push(node.path);
    else if (node.type === 'dir' && node.children?.length) notePaths(node.children, out);
  }
  return out;
}
