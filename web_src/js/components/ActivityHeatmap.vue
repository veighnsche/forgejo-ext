<script>
import {CalendarHeatmap} from 'vue3-calendar-heatmap';

export default {
  components: {CalendarHeatmap},
  props: {
    values: {
      type: Array,
      default: () => [],
    },
    locale: {
      type: Object,
      default: () => {},
    },
  },
  data: () => ({
    colorRange: [
      'var(--color-heatmap-background-1)',
      'var(--color-heatmap-background-2)',
      'var(--color-heatmap-background-3)',
      'var(--color-heatmap-background-4)',
      'var(--color-heatmap-background-5)',
      'var(--color-heatmap-background-6)',
      'var(--color-heatmap-background-7)',
    ],
    endDate: new Date(),
  }),
  mounted() {
    // work around issue with first legend color being rendered twice and legend cut off
    const legend = document.querySelector('.vch__external-legend-wrapper');
    legend.setAttribute('viewBox', '12 0 80 10');
    legend.style.marginRight = '-12px';
  },
  methods: {
    handleDayClick(e) {
      // Reset filter if same date is clicked
      const params = new URLSearchParams(document.location.search);
      const queryDate = params.get('date');
      // Timezone has to be stripped because toISOString() converts to UTC
      const clickedDate = new Date(e.date - (e.date.getTimezoneOffset() * 60000)).toISOString().substring(0, 10);

      if (queryDate && queryDate === clickedDate) {
        params.delete('date');
      } else {
        params.set('date', clickedDate);
      }

      params.delete('page');

      const newSearch = params.toString();
      window.location.search = newSearch.length ? `?${newSearch}` : '';
    },
  },
};
</script>
<template>
  <div class="total-contributions">
    {{ locale.contributions_in_the_last_12_months }}
  </div>
  <calendar-heatmap
    :locale="locale"
    :no-data-text="locale.contributions_zero"
    :tooltip-formatter="
      (v) =>
        locale.contributions_format
          .replace(
            '{contributions}',
            `<b>${v.count} ${
              v.count === 1
                ? locale.contributions_one
                : locale.contributions_few
            }</b>`
          )
          .replace('{month}', locale.months[v.date.getMonth()])
          .replace('{day}', v.date.getDate())
          .replace('{year}', v.date.getFullYear())
    "
    :end-date="endDate"
    :values="values"
    :range-color="colorRange"
    @day-click="handleDayClick($event)"
  />
</template>
