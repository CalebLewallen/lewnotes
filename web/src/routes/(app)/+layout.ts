import { redirect } from '@sveltejs/kit';

import { api, ApiError, setCSRF } from '#lib/api';
import type { LayoutLoad } from './$types';

export const load: LayoutLoad = async ({ fetch }) => {
	try {
		const me = await api.me(fetch);
		setCSRF(me.csrf_token);
		const { tree } = await api.tree(fetch);
		return { me, tree };
	} catch (error) {
		if (error instanceof ApiError && error.status === 401) redirect(307, '/login');
		throw error;
	}
};
