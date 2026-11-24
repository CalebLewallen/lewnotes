import { error } from '@sveltejs/kit';

import { api, ApiError } from '#lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	if (!params.path) error(404, 'Note not found');
	try {
		return { note: await api.noteByPath(fetch, params.path) };
	} catch (e) {
		if (e instanceof ApiError && e.status === 404) error(404, 'Note not found');
		throw e;
	}
};
