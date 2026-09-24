import type { MouseEvent as ReactMouseEvent } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';

const LANDING_SECTION_IDS = ['how', 'features', 'use-cases', 'deploy'] as const;

type LandingSectionId = (typeof LANDING_SECTION_IDS)[number];

export function useLandingMotion() {
  const pageRef = useRef<HTMLDivElement>(null);

  const headerRef = useRef<HTMLElement>(null);

  const landingScrollTargetRef = useRef<LandingSectionId | null>(null);

  const landingScrollUnlockTimerRef = useRef<number | null>(null);

  const [activeLandingSection, setActiveLandingSection] = useState<LandingSectionId>('how');

  useEffect(() => {
    const page = pageRef.current;
    if (!page) return;
    const revealTargets = Array.from(page.querySelectorAll<HTMLElement>('[data-landing-reveal]'));
    if (!revealTargets.length) return;
    const settleTimers: number[] = [];
    const reveal = (element: HTMLElement) => {
      element.classList.add('landing-reveal-visible');
      const timer = window.setTimeout(() => {
        element.classList.add('landing-reveal-settled');
      }, 1200);
      settleTimers.push(timer);
    };
    page.classList.add('landing-reveal-ready');
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      revealTargets.forEach((element) => {
        element.classList.add('landing-reveal-visible', 'landing-reveal-settled');
      });
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting) return;
          const element = entry.target as HTMLElement;
          reveal(element);
          observer.unobserve(element);
        });
      },
      {
        root: null,
        rootMargin: '0px 0px -8% 0px',
        threshold: 0.14,
      }
    );
    revealTargets.forEach((element) => observer.observe(element));
    // Feature-card spotlight: track the pointer so the radial glow follows the cursor.
    const featureCards = Array.from(
      page.querySelectorAll<HTMLElement>('.landing-features-grid article')
    );
    const finePointer = window.matchMedia('(hover: hover) and (pointer: fine)').matches;
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    let spotlightBound = false;
    const handleFeatureMove = (event: MouseEvent) => {
      const card = event.currentTarget as HTMLElement;
      const rect = card.getBoundingClientRect();
      card.style.setProperty('--feature-mx', `${event.clientX - rect.left}px`);
      card.style.setProperty('--feature-my', `${event.clientY - rect.top}px`);
    };
    if (finePointer && !reducedMotion) {
      featureCards.forEach((card) => {
        card.addEventListener('mousemove', handleFeatureMove);
      });
      spotlightBound = true;
    }
    return () => {
      observer.disconnect();
      settleTimers.forEach((timer) => window.clearTimeout(timer));
      if (spotlightBound) {
        featureCards.forEach((card) => card.removeEventListener('mousemove', handleFeatureMove));
      }
    };
  }, []);

  const scrollToLandingSection = useCallback(
    (event: ReactMouseEvent<HTMLAnchorElement>, sectionId: LandingSectionId) => {
      event.preventDefault();
      const target = document.getElementById(sectionId);
      if (!target) return;
      landingScrollTargetRef.current = sectionId;
      if (landingScrollUnlockTimerRef.current) {
        window.clearTimeout(landingScrollUnlockTimerRef.current);
      }
      setActiveLandingSection(sectionId);
      const headerHeight = headerRef.current?.getBoundingClientRect().height ?? 0;
      const top = Math.max(
        0,
        target.getBoundingClientRect().top + window.scrollY - headerHeight - 24
      );
      const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      window.history.pushState(null, '', `#${sectionId}`);
      window.scrollTo({ top, behavior: reduceMotion ? 'auto' : 'smooth' });
      landingScrollUnlockTimerRef.current = window.setTimeout(
        () => {
          if (landingScrollTargetRef.current === sectionId) {
            landingScrollTargetRef.current = null;
          }
          landingScrollUnlockTimerRef.current = null;
        },
        reduceMotion ? 0 : 1100
      );
    },
    []
  );

  // Scrollspy: highlight the top-nav link whose section is closest to the fixed header.
  useEffect(() => {
    const sections = LANDING_SECTION_IDS.map((id) => document.getElementById(id)).filter(
      (el): el is HTMLElement => Boolean(el)
    );
    if (!sections.length) return;
    let ticking = false;
    const updateActiveSection = () => {
      if (landingScrollTargetRef.current) {
        setActiveLandingSection(landingScrollTargetRef.current);
        ticking = false;
        return;
      }
      const headerHeight = headerRef.current?.getBoundingClientRect().height ?? 0;
      const offsetY = window.scrollY + headerHeight + 32;
      const atPageBottom =
        window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 2;
      let current = sections[0].id as LandingSectionId;
      for (const section of sections) {
        const sectionTop = section.getBoundingClientRect().top + window.scrollY;
        if (offsetY >= sectionTop || atPageBottom) {
          current = section.id as LandingSectionId;
        } else {
          break;
        }
      }
      setActiveLandingSection(current);
      ticking = false;
    };
    const scheduleUpdate = () => {
      if (ticking) return;
      ticking = true;
      window.requestAnimationFrame(updateActiveSection);
    };
    updateActiveSection();
    window.addEventListener('scroll', scheduleUpdate, { passive: true });
    window.addEventListener('resize', scheduleUpdate);
    window.addEventListener('popstate', scheduleUpdate);
    return () => {
      window.removeEventListener('scroll', scheduleUpdate);
      window.removeEventListener('resize', scheduleUpdate);
      window.removeEventListener('popstate', scheduleUpdate);
      if (landingScrollUnlockTimerRef.current) {
        window.clearTimeout(landingScrollUnlockTimerRef.current);
        landingScrollUnlockTimerRef.current = null;
      }
    };
  }, []);

  // Condense the floating header once the user scrolls past the hero fold.
  useEffect(() => {
    const header = headerRef.current;
    if (!header) return;
    let ticking = false;
    const onScroll = () => {
      if (ticking) return;
      ticking = true;
      window.requestAnimationFrame(() => {
        header.classList.toggle('landing-header-scrolled', window.scrollY > 80);
        ticking = false;
      });
    };
    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);
  return { pageRef, headerRef, activeLandingSection, scrollToLandingSection };
}
